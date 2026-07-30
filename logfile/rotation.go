// Package logfile owns managed-process log writing, daily rotation, and
// discovery of current and archived log files.
package logfile

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const dateLayout = "2006-01-02"

// ArchivePath returns the dated archive path for path. The date is inserted
// immediately before the final extension, so daemon.log becomes
// daemon.2026-07-29.log and daemon.err becomes daemon.2026-07-29.err.
func ArchivePath(path, date string) string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	return base + "." + date + ext
}

// Rotate moves every consecutive leading previous-date block from path into
// its dated archive. The current file keeps the first today/future block and
// every byte after it. A legacy first line without a timestamp is left intact
// because its ownership date cannot be established safely.
func Rotate(path string, now time.Time) error {
	source, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open current log %q: %w", path, err)
	}

	info, err := source.Stat()
	if err != nil {
		_ = source.Close()
		return fmt.Errorf("stat current log %q: %w", path, err)
	}

	reader := bufio.NewReader(source)
	firstLine, err := reader.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		_ = source.Close()
		return fmt.Errorf("read first line from %q: %w", path, err)
	}
	if len(firstLine) == 0 {
		return source.Close()
	}

	firstDate, ok := timestampDate(firstLine)
	today := now.Format(dateLayout)
	if !ok || firstDate >= today {
		return source.Close()
	}

	stage, err := newRotationStage(path, info.Mode())
	if err != nil {
		_ = source.Close()
		return err
	}
	defer stage.cleanup()

	if err := stage.writeArchive(firstDate, firstLine); err != nil {
		_ = source.Close()
		return err
	}

	rotating := true
	activeDate := firstDate
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			if rotating {
				if lineDate, parsed := timestampDate(line); parsed && lineDate != activeDate {
					if lineDate < today {
						activeDate = lineDate
					} else {
						rotating = false
					}
				}
			}

			if rotating {
				if err := stage.writeArchive(activeDate, line); err != nil {
					_ = source.Close()
					return err
				}
			} else if err := stage.writeCurrent(line); err != nil {
				_ = source.Close()
				return err
			}
		}

		switch {
		case readErr == nil:
		case errors.Is(readErr, io.EOF):
			if err := source.Close(); err != nil {
				return fmt.Errorf("close current log %q: %w", path, err)
			}
			return stage.commit(path)
		default:
			_ = source.Close()
			return fmt.Errorf("read current log %q: %w", path, readErr)
		}
	}
}

func timestampDate(line []byte) (string, bool) {
	var candidate string
	switch {
	case len(line) >= len(dateLayout)+1 && line[0] == '[':
		candidate = string(line[1 : len(dateLayout)+1])
	case len(line) >= len(dateLayout):
		candidate = string(line[:len(dateLayout)])
	default:
		return "", false
	}
	if _, err := time.Parse(dateLayout, candidate); err != nil {
		return "", false
	}
	return candidate, true
}

type rotationStage struct {
	mode         os.FileMode
	current      *os.File
	currentPath  string
	archives     map[string]*os.File
	archivePaths map[string]string
}

func newRotationStage(path string, mode os.FileMode) (*rotationStage, error) {
	current, err := os.CreateTemp(filepath.Dir(path), ".pm2-current-*")
	if err != nil {
		return nil, fmt.Errorf("create current-log rotation stage for %q: %w", path, err)
	}
	return &rotationStage{
		mode:         mode.Perm(),
		current:      current,
		currentPath:  current.Name(),
		archives:     make(map[string]*os.File),
		archivePaths: make(map[string]string),
	}, nil
}

func (s *rotationStage) writeArchive(date string, line []byte) error {
	file, ok := s.archives[date]
	if !ok {
		created, err := os.CreateTemp(filepath.Dir(s.currentPath), ".pm2-archive-*")
		if err != nil {
			return fmt.Errorf("create archive rotation stage for %s: %w", date, err)
		}
		s.archives[date] = created
		s.archivePaths[date] = created.Name()
		file = created
	}
	if _, err := file.Write(line); err != nil {
		return fmt.Errorf("stage archive block for %s: %w", date, err)
	}
	return nil
}

func (s *rotationStage) writeCurrent(line []byte) error {
	if _, err := s.current.Write(line); err != nil {
		return fmt.Errorf("stage current log: %w", err)
	}
	return nil
}

func (s *rotationStage) commit(path string) error {
	if err := s.closeStages(); err != nil {
		return err
	}

	dates := make([]string, 0, len(s.archivePaths))
	for date := range s.archivePaths {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	var committed []archiveRollback
	for _, date := range dates {
		target := ArchivePath(path, date)
		rollback, err := appendStage(target, s.archivePaths[date])
		if err != nil {
			rollbackArchives(committed)
			return err
		}
		committed = append(committed, rollback)
	}

	if err := os.Chmod(s.currentPath, s.mode); err != nil {
		rollbackArchives(committed)
		return fmt.Errorf("set rotated current-log mode: %w", err)
	}
	if err := os.Rename(s.currentPath, path); err != nil {
		rollbackArchives(committed)
		return fmt.Errorf("replace current log %q: %w", path, err)
	}
	s.currentPath = ""
	return nil
}

func (s *rotationStage) closeStages() error {
	if s.current != nil {
		if err := s.current.Close(); err != nil {
			return fmt.Errorf("close current-log rotation stage: %w", err)
		}
		s.current = nil
	}
	for date, file := range s.archives {
		if file == nil {
			continue
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close archive rotation stage for %s: %w", date, err)
		}
		s.archives[date] = nil
	}
	return nil
}

func (s *rotationStage) cleanup() {
	if s.current != nil {
		_ = s.current.Close()
	}
	if s.currentPath != "" {
		_ = os.Remove(s.currentPath)
	}
	for date, file := range s.archives {
		if file != nil {
			_ = file.Close()
		}
		if path := s.archivePaths[date]; path != "" {
			_ = os.Remove(path)
		}
	}
}

type archiveRollback struct {
	path    string
	size    int64
	existed bool
}

func appendStage(target, stagePath string) (archiveRollback, error) {
	rollback := archiveRollback{path: target}
	if info, err := os.Stat(target); err == nil {
		rollback.size = info.Size()
		rollback.existed = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return rollback, fmt.Errorf("stat archive %q: %w", target, err)
	}

	stage, err := os.Open(stagePath)
	if err != nil {
		return rollback, fmt.Errorf("open archive stage %q: %w", stagePath, err)
	}
	defer stage.Close()

	archive, err := os.OpenFile(target, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return rollback, fmt.Errorf("open archive %q: %w", target, err)
	}
	_, copyErr := io.Copy(archive, stage)
	closeErr := archive.Close()
	if copyErr != nil {
		rollbackArchive(rollback)
		return rollback, fmt.Errorf("append archive %q: %w", target, copyErr)
	}
	if closeErr != nil {
		rollbackArchive(rollback)
		return rollback, fmt.Errorf("close archive %q: %w", target, closeErr)
	}
	return rollback, nil
}

func rollbackArchives(archives []archiveRollback) {
	for i := len(archives) - 1; i >= 0; i-- {
		rollbackArchive(archives[i])
	}
}

func rollbackArchive(archive archiveRollback) {
	if archive.existed {
		_ = os.Truncate(archive.path, archive.size)
		return
	}
	_ = os.Remove(archive.path)
}
