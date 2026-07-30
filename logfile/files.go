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

// FileInfo describes one current or dated archive file belonging to a managed
// application's configured stdout/stderr paths.
type FileInfo struct {
	Path    string
	Name    string
	Size    int64
	ModTime time.Time
	Current bool
}

// ListRelated lists existing currentPaths and their exact
// <stem>.<YYYY-MM-DD><ext> archives. Missing directories and missing current
// files are normal empty states; unrelated files in the same directories are
// never returned.
func ListRelated(currentPaths ...string) ([]FileInfo, error) {
	current := make(map[string]struct{})
	patternsByDir := make(map[string][]filePattern)

	for _, path := range currentPaths {
		if path == "" {
			continue
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve log path %q: %w", path, err)
		}
		absolute = filepath.Clean(absolute)
		if _, exists := current[absolute]; exists {
			continue
		}
		current[absolute] = struct{}{}
		dir := filepath.Dir(absolute)
		patternsByDir[dir] = append(patternsByDir[dir], newFilePattern(filepath.Base(absolute)))
	}

	listed := make(map[string]listedFile)
	for dir, patterns := range patternsByDir {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read log directory %q: %w", dir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			archiveDate, related := matchFilePatterns(name, patterns)
			path := filepath.Join(dir, name)
			_, isCurrent := current[path]
			if !related && !isCurrent {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return nil, fmt.Errorf("stat log file %q: %w", path, err)
			}
			listed[path] = listedFile{
				FileInfo: FileInfo{
					Path:    path,
					Name:    name,
					Size:    info.Size(),
					ModTime: info.ModTime(),
					Current: isCurrent,
				},
				archiveDate: archiveDate,
			}
		}
	}

	ordered := make([]listedFile, 0, len(listed))
	for _, file := range listed {
		ordered = append(ordered, file)
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
	return files, nil
}

type filePattern struct {
	current string
	stem    string
	ext     string
}

func newFilePattern(current string) filePattern {
	ext := filepath.Ext(current)
	return filePattern{
		current: current,
		stem:    strings.TrimSuffix(current, ext),
		ext:     ext,
	}
}

func matchFilePatterns(name string, patterns []filePattern) (string, bool) {
	for _, pattern := range patterns {
		if name == pattern.current {
			return "", true
		}
		prefix := pattern.stem + "."
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, pattern.ext) {
			continue
		}
		dateEnd := len(name) - len(pattern.ext)
		if dateEnd < len(prefix) {
			continue
		}
		date := name[len(prefix):dateEnd]
		if len(date) != len(dateLayout) {
			continue
		}
		if _, err := time.Parse(dateLayout, date); err == nil {
			return date, true
		}
	}
	return "", false
}

type listedFile struct {
	FileInfo
	archiveDate string
}
