package logfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const timestampLayout = "2006-01-02 15:04:05"

// Writer adds a timestamp to every logical line and keeps path on the current
// local date. It is safe for concurrent calls to Write and Close.
type Writer struct {
	mu          sync.Mutex
	path        string
	file        *os.File
	now         func() time.Time
	atLineStart bool
	activeDate  string
	closed      bool
}

// Open opens path for timestamped append output, rotating every leading
// previous-date block before the first new line is written.
func Open(path string) (*Writer, error) {
	return openWithClock(path, time.Now)
}

func openWithClock(path string, now func() time.Time) (*Writer, error) {
	if now == nil {
		return nil, errors.New("log clock is nil")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log directory for %q: %w", path, err)
	}

	openedAt := now()
	if err := Rotate(path, openedAt); err != nil {
		return nil, fmt.Errorf("rotate log %q: %w", path, err)
	}

	file, err := openAppendFile(path)
	if err != nil {
		return nil, err
	}
	if err := ensureAppendBoundary(path, file); err != nil {
		_ = file.Close()
		return nil, err
	}

	return &Writer{
		path:        path,
		file:        file,
		now:         now,
		atLineStart: true,
		activeDate:  openedAt.Format(dateLayout),
	}, nil
}

// Write writes p to the current log and returns the number of input bytes
// consumed. Timestamp prefix bytes are deliberately excluded from the count
// required by the io.Writer contract.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, os.ErrClosed
	}
	if len(p) == 0 {
		return 0, nil
	}

	consumed := 0
	for consumed < len(p) {
		if w.atLineStart {
			lineTime := w.now()
			if err := w.ensureCurrent(lineTime); err != nil {
				return consumed, err
			}
			if _, err := fmt.Fprintf(w.file, "[%s] ", lineTime.Format(timestampLayout)); err != nil {
				return consumed, fmt.Errorf("write timestamp to %q: %w", w.path, err)
			}
			w.atLineStart = false
		}

		lineEnd := consumed
		for lineEnd < len(p) && p[lineEnd] != '\n' {
			lineEnd++
		}
		if lineEnd < len(p) {
			lineEnd++
		}

		segment := p[consumed:lineEnd]
		written, err := w.file.Write(segment)
		consumed += written
		if err != nil {
			return consumed, fmt.Errorf("write log %q: %w", w.path, err)
		}
		if written != len(segment) {
			return consumed, io.ErrShortWrite
		}
		if p[lineEnd-1] == '\n' {
			w.atLineStart = true
		}
	}
	return consumed, nil
}

// Close closes the current log file. A second Close is a no-op.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}
	w.closed = true
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	if err != nil {
		return fmt.Errorf("close log %q: %w", w.path, err)
	}
	return nil
}

func (w *Writer) ensureCurrent(lineTime time.Time) error {
	lineDate := lineTime.Format(dateLayout)
	if lineDate != w.activeDate {
		return w.reopen(lineTime, true)
	}

	targetInfo, err := os.Stat(w.path)
	if errors.Is(err, os.ErrNotExist) {
		return w.reopen(lineTime, false)
	}
	if err != nil {
		return fmt.Errorf("stat log path %q: %w", w.path, err)
	}
	currentInfo, err := w.file.Stat()
	if err != nil {
		return fmt.Errorf("stat open log %q: %w", w.path, err)
	}
	if !os.SameFile(currentInfo, targetInfo) {
		return w.reopen(lineTime, false)
	}
	return nil
}

func (w *Writer) reopen(lineTime time.Time, rotate bool) error {
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return fmt.Errorf("close log %q before reopen: %w", w.path, err)
		}
		w.file = nil
	}
	if rotate {
		if err := Rotate(w.path, lineTime); err != nil {
			return fmt.Errorf("rotate log %q at date boundary: %w", w.path, err)
		}
	}

	file, err := openAppendFile(w.path)
	if err != nil {
		return err
	}
	if err := ensureAppendBoundary(w.path, file); err != nil {
		_ = file.Close()
		return err
	}
	w.file = file
	w.activeDate = lineTime.Format(dateLayout)
	return nil
}

func openAppendFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log %q: %w", path, err)
	}
	return file, nil
}

func ensureAppendBoundary(path string, output *os.File) error {
	info, err := output.Stat()
	if err != nil {
		return fmt.Errorf("stat log %q: %w", path, err)
	}
	if info.Size() == 0 {
		return nil
	}

	input, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("inspect log boundary %q: %w", path, err)
	}
	var last [1]byte
	_, readErr := input.ReadAt(last[:], info.Size()-1)
	closeErr := input.Close()
	if readErr != nil {
		return fmt.Errorf("read log boundary %q: %w", path, readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close log boundary reader %q: %w", path, closeErr)
	}
	if last[0] == '\n' {
		return nil
	}
	if _, err := output.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("separate partial log line in %q: %w", path, err)
	}
	return nil
}
