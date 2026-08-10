package logbrowser

import (
	"strings"
	"testing"
	"time"

	"github.com/bizshuk/pm2/logfile"
)

func TestAppTreeItemShowsDirectoryNameCountAndSize(t *testing.T) {
	t.Parallel()

	m := Model{
		apps: []logfile.AppLogs{{
			App: "vidnote",
			Files: []logfile.FileInfo{
				{Name: "daemon.log", Size: 2048},
				{Name: "daemon.err", Size: 1024},
			},
		}},
		expanded: map[string]bool{"vidnote": true},
	}

	item := m.appTreeItem(0)
	for _, want := range []string{"▾", "vidnote", "2 files", "3.0 KiB"} {
		if !strings.Contains(item, want) {
			t.Errorf("app item = %q, want it to contain %q", item, want)
		}
	}
}

func TestCurrentFileTreeItemUsesDiamondMarker(t *testing.T) {
	t.Parallel()

	m := Model{
		apps: []logfile.AppLogs{{
			App: "vidnote",
			Files: []logfile.FileInfo{{
				Current: true,
				Name:    "daemon.log",
				Size:    128,
				ModTime: time.Date(2026, 7, 30, 12, 0, 0, 0, time.Local),
			}},
		}},
	}

	item := m.fileTreeItem(0, 0)
	if !strings.Contains(item, "🔶") {
		t.Fatalf("current file item missing diamond marker: %q", item)
	}
	if strings.Contains(item, "current") {
		t.Fatalf("current file item retains current label: %q", item)
	}
}
