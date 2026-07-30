package logbrowser

import (
	"strings"
	"testing"
	"time"

	"github.com/bizshuk/pm2/logfile"
	"github.com/bizshuk/pm2/process"
)

func TestApplicationTreeItemUsesBracketedIDPrefix(t *testing.T) {
	t.Parallel()

	m := Model{
		applications: []process.ProcessInfo{{
			AppConfig: process.AppConfig{
				Namespace: "default",
				Name:      "worker",
			},
			ID:     7,
			Status: process.StatusOnline,
		}},
		expanded: map[int]bool{0: true},
	}

	item := m.applicationTreeItem(0)
	if !strings.HasPrefix(item, "[7] ") {
		t.Fatalf("application item = %q, want [7] prefix", item)
	}
	if strings.Contains(item, "id 7") {
		t.Fatalf("application item retains id label: %q", item)
	}
}

func TestCurrentFileTreeItemUsesDiamondMarker(t *testing.T) {
	t.Parallel()

	m := Model{
		filesByApplication: map[int][]logfile.FileInfo{
			0: {{
				Current: true,
				Name:    "daemon.log",
				Size:    128,
				ModTime: time.Date(2026, 7, 30, 12, 0, 0, 0, time.Local),
			}},
		},
	}

	item := m.fileTreeItem(0, 0)
	if !strings.Contains(item, "🔶") {
		t.Fatalf("current file item missing diamond marker: %q", item)
	}
	if strings.Contains(item, "current") {
		t.Fatalf("current file item retains current label: %q", item)
	}
}
