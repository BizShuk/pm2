package logfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListTasksGroupsFilesByTaskName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "vidnote.log"), "current stdout")
	writeTestFile(t, filepath.Join(dir, "vidnote.err"), "current stderr")
	writeTestFile(t, filepath.Join(dir, "vidnote.2026-07-29.log"), "old")
	writeTestFile(t, filepath.Join(dir, "agentmemory.log"), "other task")

	tasks, err := ListTasks(dir)
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("ListTasks() returned %d tasks: %#v", len(tasks), tasks)
	}
	if tasks[0].Task != "agentmemory" || tasks[1].Task != "vidnote" {
		t.Fatalf("tasks not sorted by name: %q, %q", tasks[0].Task, tasks[1].Task)
	}

	vidnote := tasks[1]
	gotNames := make([]string, 0, len(vidnote.Files))
	for _, file := range vidnote.Files {
		gotNames = append(gotNames, file.Name)
		if file.Size <= 0 {
			t.Errorf("FileInfo.Size for %q = %d, want positive", file.Path, file.Size)
		}
	}
	wantNames := []string{"vidnote.err", "vidnote.log", "vidnote.2026-07-29.log"}
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

// A task whose name itself contains a dotted segment must not be split on it:
// only a trailing YYYY-MM-DD is a rotation date.
func TestListTasksKeepsDottedTaskNamesIntact(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "api.v2.log"), "current")
	writeTestFile(t, filepath.Join(dir, "api.v2.2026-07-29.log"), "old")

	tasks, err := ListTasks(dir)
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].Task != "api.v2" {
		t.Fatalf("ListTasks() = %#v, want one task named api.v2", tasks)
	}
	if len(tasks[0].Files) != 2 {
		t.Fatalf("api.v2 files = %#v, want its current file and its archive", tasks[0].Files)
	}
}

// Subdirectories are not log files. The log directory is flat by
// construction, but an unrelated directory dropped in it must not become a
// row the user can delete.
func TestListTasksIgnoresSubdirectories(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "archive"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	tasks, err := ListTasks(dir)
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("ListTasks() = %#v, want no tasks", tasks)
	}
}

// A daemon that has never launched a task has no log directory at all. That
// is an empty listing, not a failure.
func TestListTasksReturnsNoTasksForMissingDir(t *testing.T) {
	t.Parallel()

	tasks, err := ListTasks(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("ListTasks() = %#v, want no tasks", tasks)
	}
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
