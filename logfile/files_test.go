package logfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListRelatedIncludesCurrentAndDatedArchivesOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "daemon.log")
	errPath := filepath.Join(dir, "daemon.err")
	for path, content := range map[string]string{
		logPath: "current stdout",
		errPath: "current stderr",
		filepath.Join(dir, "daemon.2026-07-29.log"): "old stdout",
		filepath.Join(dir, "daemon.2026-07-29.err"): "old stderr",
		filepath.Join(dir, "daemon.not-a-date.log"): "invalid archive",
		filepath.Join(dir, "unrelated.txt"):         "unrelated",
	} {
		writeTestFile(t, path, content)
	}

	files, err := ListRelated(logPath, errPath, logPath)
	if err != nil {
		t.Fatalf("ListRelated() error = %v", err)
	}
	if len(files) != 4 {
		t.Fatalf("ListRelated() returned %d files: %#v", len(files), files)
	}

	gotNames := make([]string, 0, len(files))
	for _, file := range files {
		gotNames = append(gotNames, file.Name)
		if !filepath.IsAbs(file.Path) {
			t.Errorf("FileInfo.Path = %q, want absolute path", file.Path)
		}
		if file.Size <= 0 {
			t.Errorf("FileInfo.Size for %q = %d, want positive", file.Path, file.Size)
		}
	}
	wantNames := []string{
		"daemon.err",
		"daemon.log",
		"daemon.2026-07-29.err",
		"daemon.2026-07-29.log",
	}
	for i, want := range wantNames {
		if gotNames[i] != want {
			t.Errorf("files[%d].Name = %q, want %q (all: %v)", i, gotNames[i], want, gotNames)
		}
	}
	if !files[0].Current || !files[1].Current {
		t.Errorf("current files not marked current: %#v", files[:2])
	}
	if files[2].Current || files[3].Current {
		t.Errorf("archives marked current: %#v", files[2:])
	}
}

func TestListRelatedIgnoresMissingCurrentDirectories(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing", "daemon.log")
	files, err := ListRelated(path)
	if err != nil {
		t.Fatalf("ListRelated() error = %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("ListRelated() = %#v, want no files", files)
	}
}

func TestListRelatedSupportsExtensionlessLogs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	current := filepath.Join(dir, "worker")
	archive := filepath.Join(dir, "worker.2026-07-29")
	writeTestFile(t, current, "current")
	writeTestFile(t, archive, "archive")
	writeTestFile(t, filepath.Join(dir, "worker.invalid"), "not an archive")

	files, err := ListRelated(current)
	if err != nil {
		t.Fatalf("ListRelated() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("ListRelated() = %#v, want current and archive", files)
	}
	if files[0].Path != current {
		t.Errorf("first path = %q, want %q", files[0].Path, current)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("archive fixture missing: %v", err)
	}
}
