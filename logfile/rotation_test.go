package logfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArchivePathInsertsDateBeforeExtension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "stdout",
			path: "/tmp/app/logs/daemon.log",
			want: "/tmp/app/logs/daemon.2026-07-29.log",
		},
		{
			name: "stderr",
			path: "/tmp/app/logs/daemon.err",
			want: "/tmp/app/logs/daemon.2026-07-29.err",
		},
		{
			name: "no extension",
			path: "/tmp/app/logs/worker",
			want: "/tmp/app/logs/worker.2026-07-29",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ArchivePath(tt.path, "2026-07-29"); got != tt.want {
				t.Fatalf("ArchivePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestRotateMovesEveryLeadingPreviousDateBlock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.log")
	writeTestFile(t, path,
		"[2026-07-27 23:59:59] first\n"+
			"legacy continuation for first\n"+
			"[2026-07-28 00:00:00] second\n"+
			"[2026-07-29 00:00:00] latest\n",
	)

	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.Local)
	if err := Rotate(path, now); err != nil {
		t.Fatalf("Rotate() error = %v", err)
	}

	assertTestFile(t, path, "[2026-07-29 00:00:00] latest\n")
	assertTestFile(t, filepath.Join(dir, "daemon.2026-07-27.log"),
		"[2026-07-27 23:59:59] first\nlegacy continuation for first\n")
	assertTestFile(t, filepath.Join(dir, "daemon.2026-07-28.log"),
		"[2026-07-28 00:00:00] second\n")
}

func TestRotateAppendsToExistingArchive(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.err")
	archive := filepath.Join(dir, "daemon.2026-07-28.err")
	writeTestFile(t, archive, "[2026-07-28 01:00:00] earlier run\n")
	writeTestFile(t, path,
		"[2026-07-28 22:00:00] later run\n"+
			"[2026-07-29 00:00:00] current",
	)

	now := time.Date(2026, 7, 29, 1, 0, 0, 0, time.Local)
	if err := Rotate(path, now); err != nil {
		t.Fatalf("Rotate() error = %v", err)
	}

	assertTestFile(t, archive,
		"[2026-07-28 01:00:00] earlier run\n"+
			"[2026-07-28 22:00:00] later run\n")
	assertTestFile(t, path, "[2026-07-29 00:00:00] current")
}

func TestRotateKeepsCurrentOrLegacyFirstLine(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 29, 1, 0, 0, 0, time.Local)
	tests := []struct {
		name    string
		content string
	}{
		{name: "current date", content: "[2026-07-29 00:00:00] current\n"},
		{name: "future date", content: "[2026-07-30 00:00:00] future\n"},
		{name: "legacy", content: "untimestamped legacy output\n"},
		{name: "empty", content: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "daemon.log")
			writeTestFile(t, path, tt.content)

			if err := Rotate(path, now); err != nil {
				t.Fatalf("Rotate() error = %v", err)
			}

			assertTestFile(t, path, tt.content)
			archives, err := filepath.Glob(filepath.Join(filepath.Dir(path), "daemon.*.log"))
			if err != nil {
				t.Fatalf("Glob() error = %v", err)
			}
			if len(archives) != 0 {
				t.Fatalf("archives = %v, want none", archives)
			}
		})
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func assertTestFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("file %q = %q, want %q", path, got, want)
	}
}
