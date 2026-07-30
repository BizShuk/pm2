package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bizshuk/pm2/logfile"
	"github.com/bizshuk/pm2/process"
)

func TestLogsCommandIsStreamingMode(t *testing.T) {
	if LogsCmd.Use != "logs [name]" {
		t.Fatalf("LogsCmd.Use = %q, want %q", LogsCmd.Use, "logs [name]")
	}
	if !strings.Contains(strings.ToLower(LogsCmd.Short), "stream") {
		t.Fatalf("LogsCmd.Short = %q, want streaming description", LogsCmd.Short)
	}
	if strings.Contains(strings.ToLower(LogsCmd.Short), "interactive") {
		t.Fatalf("LogsCmd.Short = %q, root logs must not describe interactive mode", LogsCmd.Short)
	}
}

func TestLogsMonitorIsInteractiveSubcommand(t *testing.T) {
	if got := LogsMonitorCmd.Parent(); got != LogsCmd {
		t.Fatalf("LogsMonitorCmd.Parent() = %v, want LogsCmd", got)
	}
	if LogsMonitorCmd.Use != "monitor [name]" {
		t.Fatalf("LogsMonitorCmd.Use = %q, want %q", LogsMonitorCmd.Use, "monitor [name]")
	}
	if !strings.Contains(strings.ToLower(LogsMonitorCmd.Short), "interactive") {
		t.Fatalf("LogsMonitorCmd.Short = %q, want interactive description", LogsMonitorCmd.Short)
	}

	hasShortAlias := false
	for _, alias := range LogsMonitorCmd.Aliases {
		if alias == "m" {
			hasShortAlias = true
			break
		}
	}
	if !hasShortAlias {
		t.Fatalf("LogsMonitorCmd.Aliases = %v, want m", LogsMonitorCmd.Aliases)
	}

	for _, path := range [][]string{{"logs", "monitor"}, {"logs", "m"}} {
		command, _, err := Cmd.Find(path)
		if err != nil {
			t.Fatalf("find pm2 %s: %v", strings.Join(path, " "), err)
		}
		if command != LogsMonitorCmd {
			t.Fatalf("pm2 %s resolves to %q, want LogsMonitorCmd", strings.Join(path, " "), command.Name())
		}
	}
}

func TestWriteLogStreamRoutesStdoutAndStderr(t *testing.T) {
	t.Parallel()

	logTime := time.Date(2026, 7, 30, 8, 9, 10, 0, time.Local)
	entries := make(chan logfile.Entry, 2)
	entries <- logfile.Entry{
		Time:    logTime,
		AppName: "worker",
		Stream:  logfile.StreamStdout,
		Message: "completed",
	}
	entries <- logfile.Entry{
		Time:    logTime,
		AppName: "worker",
		Stream:  logfile.StreamStderr,
		Message: "failed",
	}
	close(entries)
	errs := make(chan error)
	close(errs)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := writeLogStream(context.Background(), entries, errs, &stdout, &stderr)
	if err != nil {
		t.Fatalf("writeLogStream() error = %v", err)
	}
	if got, want := stdout.String(),
		"[2026-07-30 08:09:10] worker | completed\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got, want := stderr.String(),
		"[2026-07-30 08:09:10] worker | failed\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestWriteLogStreamReturnsFollowerError(t *testing.T) {
	t.Parallel()

	entries := make(chan logfile.Entry)
	errs := make(chan error, 1)
	errs <- errors.New("permission denied")
	close(errs)

	err := writeLogStream(
		context.Background(),
		entries,
		errs,
		&bytes.Buffer{},
		&bytes.Buffer{},
	)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("writeLogStream() error = %v, want follower error", err)
	}
}

func TestWriteLogStreamTreatsCancellationAsNormalStop(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	entries := make(chan logfile.Entry)
	errs := make(chan error)
	cancel()

	err := writeLogStream(ctx, entries, errs, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("writeLogStream() cancellation error = %v, want nil", err)
	}
}

func TestLogSourcesResolveCompositeTargetAndBothStreams(t *testing.T) {
	t.Parallel()

	applications := []process.ProcessInfo{
		{
			AppConfig: process.AppConfig{
				Namespace: "production",
				Name:      "api",
				LogFile:   "/tmp/production-api.log",
				ErrorFile: "/tmp/production-api.err",
			},
			ID: 1,
		},
		{
			AppConfig: process.AppConfig{
				Namespace: "staging",
				Name:      "api",
				LogFile:   "/tmp/staging-api.log",
				ErrorFile: "/tmp/staging-api.err",
			},
			ID: 2,
		},
	}

	sources, err := logSources(applications, "staging:api")
	if err != nil {
		t.Fatalf("logSources() error = %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("logSources() returned %d sources, want 2: %#v", len(sources), sources)
	}
	if sources[0].AppName != "api" || sources[0].Stream != logfile.StreamStdout ||
		sources[0].Path != "/tmp/staging-api.log" {
		t.Errorf("stdout source = %#v, want staging api stdout", sources[0])
	}
	if sources[1].AppName != "api" || sources[1].Stream != logfile.StreamStderr ||
		sources[1].Path != "/tmp/staging-api.err" {
		t.Errorf("stderr source = %#v, want staging api stderr", sources[1])
	}
}

func TestLogSourcesRejectUnknownTarget(t *testing.T) {
	t.Parallel()

	_, err := logSources([]process.ProcessInfo{{
		AppConfig: process.AppConfig{Name: "api"},
		ID:        1,
	}}, "missing")
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("logSources() error = %v, want unknown target error", err)
	}
}
