package logfile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEntryStringIncludesTimeApplicationAndMessage(t *testing.T) {
	t.Parallel()

	entry := Entry{
		Time:    time.Date(2026, 7, 30, 8, 9, 10, 0, time.Local),
		AppName: "worker",
		Message: "completed job",
	}

	if got, want := entry.String(),
		"[2026-07-30 08:09:10] worker | completed job"; got != want {
		t.Fatalf("Entry.String() = %q, want %q", got, want)
	}
}

func TestFollowEmitsNewTimestampedLine(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "daemon.log")
	writeTestFile(t, path, "[2026-07-30 08:00:00] existing\n")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	entries, errs := Follow(ctx, []Source{{
		AppName: "worker",
		Path:    path,
		Stream:  StreamStdout,
	}})

	appendTestLog(t, path, "[2026-07-30 08:09:10] completed job\n")

	entry := receiveEntry(t, entries)
	if got, want := entry.Time,
		time.Date(2026, 7, 30, 8, 9, 10, 0, time.Local); !got.Equal(want) {
		t.Errorf("Entry.Time = %v, want %v", got, want)
	}
	if entry.AppName != "worker" {
		t.Errorf("Entry.AppName = %q, want worker", entry.AppName)
	}
	if entry.Stream != StreamStdout {
		t.Errorf("Entry.Stream = %q, want %q", entry.Stream, StreamStdout)
	}
	if entry.Message != "completed job" {
		t.Errorf("Entry.Message = %q, want completed job", entry.Message)
	}
	assertNoFollowError(t, errs)
}

func TestFollowStartsAtExistingFileEnd(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "daemon.log")
	writeTestFile(t, path, "[2026-07-30 08:00:00] existing\n")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	entries, _ := Follow(ctx, []Source{{AppName: "worker", Path: path}})

	select {
	case entry := <-entries:
		t.Fatalf("Follow() replayed existing entry: %#v", entry)
	case <-time.After(150 * time.Millisecond):
	}

	appendTestLog(t, path, "[2026-07-30 08:10:00] new\n")
	if got := receiveEntry(t, entries).Message; got != "new" {
		t.Fatalf("Entry.Message = %q, want new", got)
	}
}

func TestFollowReopensRecreatedPath(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "daemon.err")
	writeTestFile(t, path, "[2026-07-30 08:00:00] existing\n")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	entries, _ := Follow(ctx, []Source{{
		AppName: "worker",
		Path:    path,
		Stream:  StreamStderr,
	}})

	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	writeTestFile(t, path, "[2026-07-30 08:11:00] recreated\n")

	entry := receiveEntry(t, entries)
	if entry.Stream != StreamStderr {
		t.Errorf("Entry.Stream = %q, want %q", entry.Stream, StreamStderr)
	}
	if entry.Message != "recreated" {
		t.Errorf("Entry.Message = %q, want recreated", entry.Message)
	}
}

func TestFollowHoldsPartialLineUntilNewline(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "daemon.log")
	writeTestFile(t, path, "")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	entries, _ := Follow(ctx, []Source{{AppName: "worker", Path: path}})

	appendTestLog(t, path, "[2026-07-30 08:12:00] partial")
	select {
	case entry := <-entries:
		t.Fatalf("Follow() emitted partial entry: %#v", entry)
	case <-time.After(150 * time.Millisecond):
	}

	appendTestLog(t, path, " line\n")
	if got := receiveEntry(t, entries).Message; got != "partial line" {
		t.Fatalf("Entry.Message = %q, want partial line", got)
	}
}

func TestFollowCancellationClosesChannels(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "daemon.log")
	writeTestFile(t, path, "")

	ctx, cancel := context.WithCancel(context.Background())
	entries, errs := Follow(ctx, []Source{{AppName: "worker", Path: path}})
	cancel()

	assertChannelClosed(t, entries, "entries")
	assertChannelClosed(t, errs, "errors")
}

func appendTestLog(t *testing.T, path, content string) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile(%q) error = %v", path, err)
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		t.Fatalf("WriteString(%q) error = %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close(%q) error = %v", path, err)
	}
}

func receiveEntry(t *testing.T, entries <-chan Entry) Entry {
	t.Helper()

	select {
	case entry, ok := <-entries:
		if !ok {
			t.Fatal("entries channel closed before an entry arrived")
		}
		return entry
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for followed log entry")
		return Entry{}
	}
}

func assertNoFollowError(t *testing.T, errs <-chan error) {
	t.Helper()

	select {
	case err, ok := <-errs:
		if ok {
			t.Fatalf("Follow() error = %v", err)
		}
	default:
	}
}

func assertChannelClosed[T any](t *testing.T, channel <-chan T, name string) {
	t.Helper()

	select {
	case _, ok := <-channel:
		if ok {
			t.Fatalf("%s channel sent a value after cancellation", name)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s channel to close", name)
	}
}
