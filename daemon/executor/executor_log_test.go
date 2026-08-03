package executor

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
)

func TestStartTimestampsAndRotatesManagedOutput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "logs", "daemon.log")
	errPath := filepath.Join(dir, "logs", "daemon.err")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeExecutorTestFile(t, logPath,
		"[2000-01-01 01:00:00] old stdout\n"+
			"[2000-01-02 01:00:00] newer stdout\n")
	writeExecutorTestFile(t, errPath, "[2000-01-02 02:00:00] old stderr\n")

	req := &model.AppStartReq{AppConfig: process.AppConfig{
		Name:        "logger",
		Script:      `printf 'out one\nout two\n'; printf 'err one\n' >&2`,
		LogFile:     logPath,
		ErrorFile:   errPath,
		ConfigDir:   dir,
		ConfigFile:  filepath.Join(dir, "ecosystem.config.js"),
		CWD:         dir,
		BaseEnv:     os.Environ(),
		MaxRestarts: 1,
	}}

	executor := NewExecutor(dir)
	result, err := executor.Start(req, "logger", nil)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	executor.Watch(result.Cmd, result.OutF, result.ErrF, result.Watcher, nil, nil)

	assertExecutorFile(t, filepath.Join(dir, "logs", "daemon.2000-01-01.log"),
		"[2000-01-01 01:00:00] old stdout\n")
	assertExecutorFile(t, filepath.Join(dir, "logs", "daemon.2000-01-02.log"),
		"[2000-01-02 01:00:00] newer stdout\n")
	assertExecutorFile(t, filepath.Join(dir, "logs", "daemon.2000-01-02.err"),
		"[2000-01-02 02:00:00] old stderr\n")

	assertTimestampedExecutorLines(t, logPath, []string{"out one\\n", "out two\\n"})
	assertTimestampedExecutorLines(t, errPath, []string{"err one\\n"})
}

func writeExecutorTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func assertExecutorFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("file %q = %q, want %q", path, got, want)
	}
}

func assertTimestampedExecutorLines(t *testing.T, path string, messages []string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	if len(lines) != len(messages) {
		t.Fatalf("file %q lines = %q, want %d lines", path, lines, len(messages))
	}
	prefix := regexp.MustCompile(`^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\] `)
	for i, line := range lines {
		if !prefix.MatchString(line) {
			t.Errorf("line %d = %q, missing timestamp", i, line)
			continue
		}
		if got := prefix.ReplaceAllString(line, ""); got != messages[i] {
			t.Errorf("line %d message = %q, want %q", i, got, messages[i])
		}
	}
}
