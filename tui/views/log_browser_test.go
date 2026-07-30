package views

import (
	"strings"
	"testing"
)

func TestRenderLogBrowserApplicationList(t *testing.T) {
	t.Parallel()

	output := RenderLogBrowser(LogBrowserContext{
		Width:      80,
		Height:     12,
		Breadcrumb: []string{"applications"},
		Items:      []string{"default:api", "default:worker"},
		Selected:   1,
	})

	for _, want := range []string{
		"pm2 logs",
		"applications",
		"default:api",
		"default:worker",
		"navigate",
		"open",
		"quit",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q: %q", want, output)
		}
	}
}

func TestRenderLogBrowserViewerShowsVisibleCursor(t *testing.T) {
	t.Parallel()

	output := RenderLogBrowser(LogBrowserContext{
		Width:      60,
		Height:     8,
		Breadcrumb: []string{"applications", "worker", "daemon.log"},
		Lines:      []string{"line 0", "line 1", "line 2", "line 3", "line 4"},
		LineCursor: 4,
		Viewer:     true,
	})

	if !strings.Contains(output, "line 4") {
		t.Fatalf("viewer missing selected latest line: %q", output)
	}
	if !strings.Contains(output, "↑↓ / jk") {
		t.Errorf("viewer missing navigation hint: %q", output)
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
