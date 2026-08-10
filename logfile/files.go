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

// LogsDirName is the per-application log directory every application owns
// under the shared config root (~/.config/<app>/logs).
const LogsDirName = "logs"

// FileInfo describes one log file: either a current file or one of its
// <stem>.<YYYY-MM-DD><ext> daily archives.
type FileInfo struct {
	Path    string
	Name    string
	Size    int64
	ModTime time.Time
	Current bool
}

// AppLogs groups every log file found in one application's log directory.
type AppLogs struct {
	App   string
	Dir   string
	Files []FileInfo
}

// TotalSize sums every listed file in the application's log directory.
func (a AppLogs) TotalSize() int64 {
	var total int64
	for _, file := range a.Files {
		total += file.Size
	}
	return total
}

// ListApps scans root for <app>/logs directories and returns one AppLogs per
// application that owns at least one log file, ordered by application name.
//
// The scan is deliberately keyed on the log directory rather than on the
// daemon's process list: an application's logs outlive the task that wrote
// them, and a task that was deleted — or never registered with this daemon —
// still has logs worth reading. A missing root, a missing log directory, and
// an unreadable one are all ordinary empty states; one root-owned config
// directory must not blank the whole listing.
func ListApps(root string) ([]AppLogs, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config root %q: %w", root, err)
	}

	apps := make([]AppLogs, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name(), LogsDirName)
		files := listDir(dir)
		if len(files) == 0 {
			continue
		}
		apps = append(apps, AppLogs{App: entry.Name(), Dir: dir, Files: files})
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].App < apps[j].App })
	return apps, nil
}

// listDir returns every regular file in dir, current files first and archives
// newest first. Unreadable directories and unstattable entries are skipped.
func listDir(dir string) []FileInfo {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	type listed struct {
		FileInfo
		archiveDate string
	}
	ordered := make([]listed, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		name := entry.Name()
		date := archiveDate(name)
		ordered = append(ordered, listed{
			FileInfo: FileInfo{
				Path:    filepath.Join(dir, name),
				Name:    name,
				Size:    info.Size(),
				ModTime: info.ModTime(),
				Current: date == "",
			},
			archiveDate: date,
		})
	}

	sort.Slice(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if left.Current != right.Current {
			return left.Current
		}
		if left.archiveDate != right.archiveDate {
			return left.archiveDate > right.archiveDate
		}
		return left.Name < right.Name
	})

	files := make([]FileInfo, len(ordered))
	for i, file := range ordered {
		files[i] = file.FileInfo
	}
	return files
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
