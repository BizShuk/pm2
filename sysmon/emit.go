package sysmon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/bizshuk/pm2/process"
)

// DefaultEmitInterval is the period used when none is configured.
const DefaultEmitInterval = 30 * time.Second

// TaskSource supplies the managed applications to join into each
// snapshot. It is a function rather than a daemon client so sysmon stays
// independent of the RPC layer: the CLI passes a closure over its own
// client, and tests pass a literal slice.
//
// An error is reported inside the emitted snapshot rather than stopping
// the loop — a dashboard feed should survive a daemon restart.
type TaskSource func() ([]process.ProcessInfo, error)

// SnapshotEncoder writes one snapshot to the stream. Encoders own all
// presentation; sysmon itself only knows how to serialise to JSON.
type SnapshotEncoder func(io.Writer, Snapshot) error

// Emitter writes a complete system snapshot on a fixed period — the
// non-interactive half of the dashboard, meant to be redirected into a
// log file or piped into a collector.
type Emitter struct {
	Collector *Collector
	Tasks     TaskSource
	Encoder   SnapshotEncoder
	Interval  time.Duration
	// Count bounds how many snapshots are written. Zero runs until the
	// context is cancelled.
	Count int
}

// NewEmitter returns an emitter with the default JSON-lines encoder and
// its own Collector. Callers override any field before calling Run.
func NewEmitter(tasks TaskSource) *Emitter {
	return &Emitter{
		Collector: New(),
		Tasks:     tasks,
		Encoder:   EncodeJSONLine,
		Interval:  DefaultEmitInterval,
	}
}

// Run writes one snapshot immediately, then one per interval until the
// count is reached or ctx is cancelled. Cancelling is a normal stop, not
// an error: the caller wired SIGINT to it.
func (e *Emitter) Run(ctx context.Context, out io.Writer) error {
	interval := e.Interval
	if interval <= 0 {
		interval = DefaultEmitInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for written := 0; e.Count <= 0 || written < e.Count; written++ {
		if err := e.Emit(out); err != nil {
			return err
		}
		if e.Count > 0 && written+1 >= e.Count {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
	return nil
}

// Emit writes exactly one snapshot.
func (e *Emitter) Emit(out io.Writer) error {
	snapshot := e.snapshot()
	encode := e.Encoder
	if encode == nil {
		encode = EncodeJSONLine
	}
	if err := encode(out, snapshot); err != nil {
		return fmt.Errorf("emit snapshot: %w", err)
	}
	return nil
}

// snapshot collects one observation, folding a task-source failure into
// the snapshot's error list so the machine-level readings still ship.
func (e *Emitter) snapshot() Snapshot {
	var (
		managed  []process.ProcessInfo
		taskErr  error
		snapshot Snapshot
	)
	if e.Tasks != nil {
		managed, taskErr = e.Tasks()
	}
	snapshot = e.Collector.Snapshot(managed)
	if taskErr != nil {
		snapshot.Errors = append(snapshot.Errors, taskErr.Error())
	}
	return snapshot
}

// EncodeJSONLine writes a snapshot as one newline-delimited JSON object,
// the shape a log shipper or `jq` consumes without extra framing.
func EncodeJSONLine(out io.Writer, snapshot Snapshot) error {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if _, err := out.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return nil
}
