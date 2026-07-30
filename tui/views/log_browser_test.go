package views

import (
	"strings"
	"testing"
)

func TestRenderLogBrowserTreeExplorer(t *testing.T) {
	t.Parallel()

	output := RenderLogBrowser(LogBrowserContext{
		Width:      80,
		Height:     12,
		Breadcrumb: []string{"log files"},
		Items:      []string{"[1] ▸ default:api", "[2] ▾ default:worker", "    🔶 daemon.log"},
		Selected:   1,
	})

	for _, want := range []string{
		"pm2 logs monitor",
		"log files",
		"TREE",
		"LOG",
		"default:api",
		"default:worker",
		"daemon.log",
		"Select a log file and press Enter",
		"navigate",
		"← collapse/back",
		"→ expand/open",
		"Enter focus log",
		"quit",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q: %q", want, output)
		}
	}
	if strings.Contains(output, "d delete") {
		t.Errorf("application row unexpectedly shows delete hint: %q", output)
	}
}

func TestRenderLogBrowserTreeFileShowsDeleteHint(t *testing.T) {
	t.Parallel()

	output := RenderLogBrowser(LogBrowserContext{
		Width:      100,
		Height:     12,
		Breadcrumb: []string{"log files"},
		Items:      []string{"▾ default:worker", "    daemon.log"},
		Selected:   1,
		CanDelete:  true,
	})

	if !strings.Contains(output, "d delete") {
		t.Fatalf("file row missing delete hint: %q", output)
	}
}

func TestRenderLogBrowserViewerShowsVisibleCursor(t *testing.T) {
	t.Parallel()

	output := RenderLogBrowser(LogBrowserContext{
		Width:      60,
		Height:     8,
		Breadcrumb: []string{"applications", "worker", "daemon.log"},
		Items:      []string{"[7] ▾ default:worker", "    🔶 daemon.log"},
		Selected:   1,
		Lines:      []string{"line 0", "line 1", "line 2", "line 3", "line 4"},
		LineCursor: 4,
		ViewerPath: "/tmp/daemon.log",
		Viewer:     true,
	})

	if !strings.Contains(output, "default:worker") {
		t.Fatalf("focused Viewer no longer shows Tree pane: %q", output)
	}
	if !strings.Contains(output, "line 4") {
		t.Fatalf("viewer missing selected latest line: %q", output)
	}
	if !strings.Contains(output, "LOG daemon.log") {
		t.Errorf("viewer missing active log heading: %q", output)
	}
	if !strings.Contains(output, "↑↓ / jk") {
		t.Errorf("viewer missing navigation hint: %q", output)
	}
	if !strings.Contains(output, "PgUp/PgDn page") {
		t.Errorf("viewer missing page navigation hint: %q", output)
	}
	if !strings.Contains(output, "← focus tree") {
		t.Errorf("viewer missing Left back hint: %q", output)
	}
	if strings.Contains(output, "delete") {
		t.Errorf("viewer unexpectedly exposes delete action: %q", output)
	}
}

func TestRenderLogBrowserDeleteConfirmation(t *testing.T) {
	t.Parallel()

	output := RenderLogBrowser(LogBrowserContext{
		Width:       80,
		Height:      10,
		Breadcrumb:  []string{"applications", "worker", "logs"},
		ConfirmPath: "/tmp/daemon.2026-07-29.log",
	})

	for _, want := range []string{
		"Delete /tmp/daemon.2026-07-29.log?",
		"y/N",
		"confirm delete",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("confirmation missing %q: %q", want, output)
		}
	}
}

func TestRenderLogBrowserDeleteConfirmationKeepsChoiceForLongPath(t *testing.T) {
	t.Parallel()

	longPath := "/Users/shuk/projects/tools/pm2/tmp/log-smoke/app/logs/" +
		"daemon.2000-01-02.err"
	output := RenderLogBrowser(LogBrowserContext{
		Width:       80,
		Height:      10,
		Breadcrumb:  []string{"applications", "worker", "logs"},
		ConfirmPath: longPath,
	})

	if !strings.Contains(output, "? [y/N]") {
		t.Fatalf("long-path confirmation lost explicit choice: %q", output)
	}
	if !strings.Contains(output, "daemon.2000-01-02.err") {
		t.Fatalf("long-path confirmation lost selected filename: %q", output)
	}
}
