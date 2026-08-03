package sysmon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/bizshuk/pm2/process"
)

// stubEmitter returns an emitter that never touches the OS, so the loop
// contract can be tested without a one-second iostat on every iteration.
func stubEmitter(tasks TaskSource) *Emitter {
	emitter := NewEmitter(tasks)
	emitter.Collector = &Collector{sampler: newFallbackSampler()}
	emitter.Interval = time.Millisecond
	return emitter
}

func TestEmitterWritesOneRecordPerSnapshot(t *testing.T) {
	emitter := stubEmitter(func() ([]process.ProcessInfo, error) { return nil, nil })
	emitter.Count = 3

	var out bytes.Buffer
	if err := emitter.Run(context.Background(), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want one self-contained record per snapshot", len(lines))
	}
	for index, line := range lines {
		var snapshot Snapshot
		if err := json.Unmarshal([]byte(line), &snapshot); err != nil {
			t.Fatalf("line %d is not valid JSON: %v", index, err)
		}
		if snapshot.Time.IsZero() {
			t.Errorf("line %d has no timestamp", index)
		}
	}
}

func TestEmitterEmitsImmediately(t *testing.T) {
	// A user running the command interactively should not wait a full
	// interval to see whether it works.
	emitter := stubEmitter(nil)
	emitter.Interval = time.Hour
	emitter.Count = 1

	var out bytes.Buffer
	start := time.Now()
	if err := emitter.Run(context.Background(), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("first snapshot took %s; it should not wait for the interval", elapsed)
	}
	if out.Len() == 0 {
		t.Error("no output written")
	}
}

func TestEmitterStopsOnContextCancel(t *testing.T) {
	emitter := stubEmitter(nil)
	emitter.Interval = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Cancellation is a normal stop: the caller wired SIGINT to it.
	if err := emitter.Run(ctx, io.Discard); err != nil {
		t.Errorf("Run after cancel = %v, want nil", err)
	}
}

func TestEmitterSurvivesAnUnreachableDaemon(t *testing.T) {
	// Losing the task list must not cost the machine readings — that is
	// the difference between a degraded feed and a dead one.
	emitter := stubEmitter(func() ([]process.ProcessInfo, error) {
		return nil, errors.New("daemon not running")
	})
	emitter.Count = 1

	var out bytes.Buffer
	if err := emitter.Run(context.Background(), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var snapshot Snapshot
	if err := json.Unmarshal(out.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(snapshot.Errors) == 0 {
		t.Fatal("snapshot recorded no error for the unreachable daemon")
	}
	found := false
	for _, failure := range snapshot.Errors {
		if strings.Contains(failure, "daemon not running") {
			found = true
		}
	}
	if !found {
		t.Errorf("errors = %v, want the task-source failure reported", snapshot.Errors)
	}
}

func TestEmitterUsesTheConfiguredEncoder(t *testing.T) {
	emitter := stubEmitter(nil)
	emitter.Count = 1
	emitter.Encoder = func(out io.Writer, _ Snapshot) error {
		_, err := io.WriteString(out, "custom\n")
		return err
	}

	var out bytes.Buffer
	if err := emitter.Run(context.Background(), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.String() != "custom\n" {
		t.Errorf("output = %q, want the custom encoder's output", out.String())
	}
}
