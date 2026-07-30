package logfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriterPrefixesEveryLogicalLineAcrossChunkBoundaries(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "daemon.log")
	now := time.Date(2026, 7, 30, 8, 9, 10, 0, time.Local)
	writer, err := openWithClock(path, func() time.Time { return now })
	if err != nil {
		t.Fatalf("openWithClock() error = %v", err)
	}

	assertWriteCount(t, writer, "one\ntw")
	assertWriteCount(t, writer, "o\n")
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	assertTestFile(t, path,
		"[2026-07-30 08:09:10] one\n"+
			"[2026-07-30 08:09:10] two\n")
}

func TestWriterTimestampsFinalPartialLine(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "daemon.err")
	now := time.Date(2026, 7, 30, 11, 12, 13, 0, time.Local)
	writer, err := openWithClock(path, func() time.Time { return now })
	if err != nil {
		t.Fatalf("openWithClock() error = %v", err)
	}

	assertWriteCount(t, writer, "partial")
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	assertTestFile(t, path, "[2026-07-30 11:12:13] partial")
}

func TestWriterOpenRotatesPreviousDates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.log")
	writeTestFile(t, path,
		"[2026-07-28 10:00:00] old\n"+
			"[2026-07-29 10:00:00] yesterday\n")
	now := time.Date(2026, 7, 30, 0, 1, 2, 0, time.Local)

	writer, err := openWithClock(path, func() time.Time { return now })
	if err != nil {
		t.Fatalf("openWithClock() error = %v", err)
	}
	assertWriteCount(t, writer, "current\n")
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	assertTestFile(t, filepath.Join(dir, "daemon.2026-07-28.log"),
		"[2026-07-28 10:00:00] old\n")
	assertTestFile(t, filepath.Join(dir, "daemon.2026-07-29.log"),
		"[2026-07-29 10:00:00] yesterday\n")
	assertTestFile(t, path, "[2026-07-30 00:01:02] current\n")
}

func TestWriterRotatesWhenNextLineCrossesMidnight(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.log")
	clock := newTestClock(
		time.Date(2026, 7, 29, 23, 59, 58, 0, time.Local),
		time.Date(2026, 7, 29, 23, 59, 59, 0, time.Local),
		time.Date(2026, 7, 30, 0, 0, 1, 0, time.Local),
	)

	writer, err := openWithClock(path, clock.Now)
	if err != nil {
		t.Fatalf("openWithClock() error = %v", err)
	}
	assertWriteCount(t, writer, "before\n")
	assertWriteCount(t, writer, "after\n")
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	assertTestFile(t, filepath.Join(dir, "daemon.2026-07-29.log"),
		"[2026-07-29 23:59:59] before\n")
	assertTestFile(t, path, "[2026-07-30 00:00:01] after\n")
}

func TestWriterReopensCurrentPathAfterDeletion(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "daemon.log")
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.Local)
	writer, err := openWithClock(path, func() time.Time { return now })
	if err != nil {
		t.Fatalf("openWithClock() error = %v", err)
	}
	defer writer.Close()

	assertWriteCount(t, writer, "before delete\n")
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	assertWriteCount(t, writer, "after delete\n")

	assertTestFile(t, path, "[2026-07-30 08:00:00] after delete\n")
}

func TestWriterSeparatesAnExistingPartialLine(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "daemon.log")
	writeTestFile(t, path, "[2026-07-30 07:00:00] previous partial")
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.Local)

	writer, err := openWithClock(path, func() time.Time { return now })
	if err != nil {
		t.Fatalf("openWithClock() error = %v", err)
	}
	assertWriteCount(t, writer, "next process\n")
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	assertTestFile(t, path,
		"[2026-07-30 07:00:00] previous partial\n"+
			"[2026-07-30 08:00:00] next process\n")
}

func assertWriteCount(t *testing.T, writer *Writer, value string) {
	t.Helper()
	written, err := writer.Write([]byte(value))
	if err != nil {
		t.Fatalf("Write(%q) error = %v", value, err)
	}
	if written != len(value) {
		t.Fatalf("Write(%q) = %d, want %d", value, written, len(value))
	}
}

type testClock struct {
	times []time.Time
	index int
}

func newTestClock(times ...time.Time) *testClock {
	return &testClock{times: times}
}

func (c *testClock) Now() time.Time {
	if c.index >= len(c.times) {
		return c.times[len(c.times)-1]
	}
	now := c.times[c.index]
	c.index++
	return now
}
