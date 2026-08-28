package runhistory

import (
	"os"
	"path/filepath"
	"time"
)

// Prune deletes journal day files older than keepDays in both journals.
// It is called once at daemon start and again whenever an append opens a
// new day file — once per day in practice, which is why this package
// needs no ticker and no goroutine of its own.
//
// keepDays <= 0 keeps everything.
func (s *Store) Prune(keepDays int) error {
	now := time.Now()
	var firstErr error
	for _, dir := range []string{TasksDir(s.root), WorkflowsDir(s.root)} {
		if err := pruneDir(dir, keepDays, now); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// pruneDir removes day files whose own date — not their mtime — falls
// outside the window. Dating by name means a file copied or restored
// out of order is still judged by the day it describes.
func pruneDir(dir string, keepDays int, now time.Time) error {
	if keepDays <= 0 {
		return nil
	}
	cutoff := now.AddDate(0, 0, -keepDays)

	var firstErr error
	for _, path := range dayFiles(dir) {
		name := filepath.Base(path)
		day, err := time.ParseInLocation(dayLayout, name[:len(dayLayout)], time.Local)
		if err != nil {
			continue
		}
		if day.Before(cutoff) {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
