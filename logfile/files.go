package logfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FileInfo describes one log file: either a current file or one of its
// <stem>.<YYYY-MM-DD><ext> daily archives.
type FileInfo struct {
	Path    string
	Name    string
	Size    int64
	ModTime time.Time
	Current bool
}

// TaskLogs groups the current and archived log files belonging to one task.
type TaskLogs struct {
	Task  string
	Files []FileInfo
}

// TotalSize sums every log file the task owns.
func (a TaskLogs) TotalSize() int64 {
	var total int64
	for _, file := range a.Files {
		total += file.Size
	}
	return total
}

// ListTasks reads the shared managed-task log directory and returns one
// TaskLogs per task that owns at least one file, ordered by task name.
//
// Grouping is by filename stem, not by directory: every task writes
// <task>.log / <task>.err plus their <task>.<YYYY-MM-DD> archives into one
// flat directory, so the stem is the task identity. The scan is deliberately
// not keyed on the daemon's process list — a task's logs outlive the task
// that wrote them, and a deleted or never-registered task still has files
// worth reading. A missing or unreadable directory is an ordinary empty
// state, not an error.
func ListTasks(dir string) ([]TaskLogs, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read task log dir %q: %w", dir, err)
	}

	grouped := make(map[string][]FileInfo)
	for _, file := range listEntries(dir, entries) {
		grouped[taskStem(file.Name)] = append(grouped[taskStem(file.Name)], file)
	}

	tasks := make([]TaskLogs, 0, len(grouped))
	for name, files := range grouped {
		sortFiles(files)
		tasks = append(tasks, TaskLogs{Task: name, Files: files})
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Task < tasks[j].Task })
	return tasks, nil
}

// taskStem strips the stream extension and any rotation date from a log file
// name, leaving the task name: worker.err, worker.log and
// worker.2026-07-29.log all belong to "worker".
func taskStem(name string) string {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if date := trailingDate(stem); date != "" {
		stem = stem[:len(stem)-len(date)-1]
	}
	return stem
}

// listEntries turns directory entries into FileInfo values, skipping
// directories and unstattable entries.
func listEntries(dir string, entries []os.DirEntry) []FileInfo {
	files := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		name := entry.Name()
		files = append(files, FileInfo{
			Path:    filepath.Join(dir, name),
			Name:    name,
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Current: archiveDate(name) == "",
		})
	}
	return files
}

// sortFiles orders a task's files current-first, then archives newest first.
func sortFiles(files []FileInfo) {
	sort.Slice(files, func(i, j int) bool {
		left, right := files[i], files[j]
		if left.Current != right.Current {
			return left.Current
		}
		leftDate, rightDate := archiveDate(left.Name), archiveDate(right.Name)
		if leftDate != rightDate {
			return leftDate > rightDate
		}
		return left.Name < right.Name
	})
}

// archiveDate returns the YYYY-MM-DD segment a rotated file carries before its
// extension, or "" for a current file. Extensionless logs keep their date in
// the extension slot (worker.2026-07-29), so both positions are checked.
func archiveDate(name string) string {
	if date := trailingDate(strings.TrimSuffix(name, filepath.Ext(name))); date != "" {
		return date
	}
	return trailingDate(name)
}

func trailingDate(stem string) string {
	if len(stem) <= len(dateLayout) {
		return ""
	}
	candidate := stem[len(stem)-len(dateLayout):]
	if stem[len(stem)-len(dateLayout)-1] != '.' {
		return ""
	}
	if _, err := time.Parse(dateLayout, candidate); err != nil {
		return ""
	}
	return candidate
}
