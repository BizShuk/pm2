package runhistory

import "time"

// Event names what produced a task journal line.
type Event string

const (
	// EventRun is a task run that finished — the ordinary case, and the
	// only one that carries an exit code.
	EventRun Event = "run"
	// EventCronSkip is a cron fire dropped because the previous run of
	// the same task had not finished.
	EventCronSkip Event = "cron_skip"
	// EventLaunchFail is a fire that never produced a child process.
	EventLaunchFail Event = "launch_fail"
)

// Status is the outcome of a run, shared by both journals.
type Status string

const (
	StatusSuccess   Status = "success"
	StatusFailed    Status = "failed"
	StatusSkipped   Status = "skipped"
	StatusCancelled Status = "cancelled"
)

// Trigger names why a run started. Task triggers are stamped by the
// daemon at launch time and are deliberately not part of the wire
// protocol: a CLI must not be able to claim its start was a cron fire.
const (
	TriggerManual      = "manual"
	TriggerCron        = "cron"
	TriggerCronRestart = "cron_restart"
	TriggerWatch       = "watch"
	TriggerAutoRestart = "autorestart"
	TriggerResurrect   = "resurrect"
	TriggerRestart     = "restart"
	TriggerWebhook     = "webhook"
	TriggerNested      = "nested"
)

// TaskRecord is one line of <root>/tasks/runs/YYYY-MM-DD.jsonl.
//
// The JSON tags are an on-disk contract other tools read; do not rename
// or reorder them without a migration plan. Pinned by TestTaskRecordSchema.
type TaskRecord struct {
	TS    time.Time `json:"ts"`
	Kind  string    `json:"kind"` // always KindTask
	Event Event     `json:"event"`
	RunID string    `json:"run_id"`

	Namespace string `json:"ns"`
	Name      string `json:"name"`
	ID        int    `json:"id,omitempty"`
	Trigger   string `json:"trigger"`

	PID        int       `json:"pid,omitempty"`
	StartedAt  time.Time `json:"started_at,omitzero"`
	DurationMS int64     `json:"duration_ms,omitempty"`

	// ExitCode is a pointer because "unknown" and "exited 0" are
	// different facts. A launch failure has no exit code at all, and a
	// signalled process has no code of its own; writing either as 0
	// would report every killed job as a success.
	ExitCode *int   `json:"exit_code"`
	Signal   string `json:"signal,omitempty"`
	Restarts int    `json:"restarts,omitempty"`

	Status Status `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// StageRecord is one stage of a workflow run.
type StageRecord struct {
	Name       string    `json:"name"`
	Kind       string    `json:"kind"` // script | task | workflow
	Ref        string    `json:"ref,omitempty"`
	Status     Status    `json:"status"`
	ExitCode   *int      `json:"exit_code"`
	Signal     string    `json:"signal,omitempty"`
	StartedAt  time.Time `json:"started_at,omitzero"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Log        string    `json:"log,omitempty"` // basename under workflows/logs
	ChildRunID string    `json:"child_run_id,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// WorkflowRecord is one line of <root>/workflows/runs/YYYY-MM-DD.jsonl.
type WorkflowRecord struct {
	RunID string `json:"run_id"`
	Kind  string `json:"kind"` // always KindWorkflow

	Workflow string `json:"workflow"` // "<category>:<name>"
	Category string `json:"category"`
	Name     string `json:"name"`
	Trigger  string `json:"trigger"`

	ParentRunID string `json:"parent_run_id,omitempty"`
	Depth       int    `json:"depth,omitempty"`

	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	DurationMS int64     `json:"duration_ms"`

	Status Status            `json:"status"`
	Params map[string]string `json:"params,omitempty"`
	Stages []StageRecord     `json:"stages"`
	Error  string            `json:"error,omitempty"`

	// Truncated marks a record that exceeded maxRecordBytes and had its
	// params and per-stage detail dropped so the line stays a single
	// atomic write.
	Truncated bool `json:"truncated,omitempty"`
}

const (
	KindTask     = "task"
	KindWorkflow = "workflow"
)

// RecordTime and RecordID give the store a uniform way to date and
// identify a record without caring which journal it belongs to.
func (r TaskRecord) RecordTime() time.Time     { return r.TS }
func (r TaskRecord) RecordID() string          { return r.RunID }
func (r WorkflowRecord) RecordTime() time.Time { return r.FinishedAt }
func (r WorkflowRecord) RecordID() string      { return r.RunID }

// ExitCodeOf is a convenience for building records from an int that is
// only meaningful when known.
func ExitCodeOf(code int, known bool) *int {
	if !known {
		return nil
	}
	c := code
	return &c
}
