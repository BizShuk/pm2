package logfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListAppsGroupsLogsByApplicationDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeNestedTestFile(t, filepath.Join(root, "vidnote", LogsDirName, "daemon.log"), "current stdout")
	writeNestedTestFile(t, filepath.Join(root, "vidnote", LogsDirName, "daemon.err"), "current stderr")
	writeNestedTestFile(t, filepath.Join(root, "vidnote", LogsDirName, "daemon.2026-07-29.log"), "old")
	writeNestedTestFile(t, filepath.Join(root, "agentmemory", LogsDirName, "daemon.log"), "other app")

	apps, err := ListApps(root)
	if err != nil {
		t.Fatalf("ListApps() error = %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("ListApps() returned %d apps: %#v", len(apps), apps)
	}
	if apps[0].App != "agentmemory" || apps[1].App != "vidnote" {
		t.Fatalf("apps not sorted by name: %q, %q", apps[0].App, apps[1].App)
	}

	vidnote := apps[1]
	if vidnote.Dir != filepath.Join(root, "vidnote", LogsDirName) {
		t.Errorf("AppLogs.Dir = %q, want the app's logs directory", vidnote.Dir)
	}
	gotNames := make([]string, 0, len(vidnote.Files))
	for _, file := range vidnote.Files {
		gotNames = append(gotNames, file.Name)
		if file.Size <= 0 {
			t.Errorf("FileInfo.Size for %q = %d, want positive", file.Path, file.Size)
		}
	}
	wantNames := []string{"daemon.err", "daemon.log", "daemon.2026-07-29.log"}
	for i, want := range wantNames {
		if gotNames[i] != want {
			t.Errorf("files[%d].Name = %q, want %q (all: %v)", i, gotNames[i], want, gotNames)
		}
	}
	if !vidnote.Files[0].Current || !vidnote.Files[1].Current {
		t.Errorf("current files not marked current: %#v", vidnote.Files[:2])
	}
	if vidnote.Files[2].Current {
		t.Errorf("archive marked current: %#v", vidnote.Files[2])
	}
	if got, want := vidnote.TotalSize(), int64(len("current stdout")+len("current stderr")+len("old")); got != want {
		t.Errorf("TotalSize() = %d, want %d", got, want)
	}
}

// An application directory without a logs directory, and one whose logs
// directory is empty, are both ordinary states — neither may produce a row the
// user can only expand into nothing.
func TestListAppsSkipsApplicationsWithoutLogFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "empty", LogsDirName), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "data-only", "data"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeNestedTestFile(t, filepath.Join(root, "data-only", "config.json"), "{}")

	apps, err := ListApps(root)
	if err != nil {
		t.Fatalf("ListApps() error = %v", err)
	}
	if len(apps) != 0 {
		t.Fatalf("ListApps() = %#v, want no apps", apps)
	}
}

// Only <app>/logs is scanned. A .log file elsewhere in a config directory
// belongs to whatever wrote it (a browser cache, a database journal) and must
// not be offered for deletion.
func TestListAppsIgnoresFilesOutsideTheLogsDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeNestedTestFile(t, filepath.Join(root, "editor", "data", "Session Storage", "000003.log"), "leveldb")
	writeNestedTestFile(t, filepath.Join(root, "editor", "stray.log"), "stray")

	apps, err := ListApps(root)
	if err != nil {
		t.Fatalf("ListApps() error = %v", err)
	}
	if len(apps) != 0 {
		t.Fatalf("ListApps() = %#v, want no apps", apps)
	}
}

func TestListAppsReturnsNoAppsForMissingRoot(t *testing.T) {
	t.Parallel()

	apps, err := ListApps(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("ListApps() error = %v", err)
	}
	if len(apps) != 0 {
		t.Fatalf("ListApps() = %#v, want no apps", apps)
	}
}

func writeNestedTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	writeTestFile(t, path, content)
}

func TestArchiveDateClassifiesCurrentAndRotatedNames(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"daemon.log":                "",
		"daemon.err":                "",
		"worker.out.log":            "",
		"daemon.not-a-date.log":     "",
		"worker":                    "",
		"daemon.2026-07-29.log":     "2026-07-29",
		"daemon.2026-07-29.err":     "2026-07-29",
		"worker.out.2026-07-29.log": "2026-07-29",
		"worker.2026-07-29":         "2026-07-29",
	}
	for name, want := range cases {
		if got := archiveDate(name); got != want {
			t.Errorf("archiveDate(%q) = %q, want %q", name, got, want)
		}
	}
}
