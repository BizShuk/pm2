package web

import (
	"time"

	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/runhistory"
)

const (
	// DefaultHost is every interface. The product asked for a publicly
	// reachable dashboard and webhook; see the package doc for what that
	// implies.
	DefaultHost = "0.0.0.0"

	// DefaultPort sits in the workspace's public segment (8300-8399),
	// taking its smallest unused number.
	DefaultPort = 8301
)

// Backend is the only contract the HTTP layer gets from the daemon. It
// is the import-cycle guard: web never imports daemon, exactly as
// network never does.
//
// The workflow view types are declared here rather than imported from
// the workflow package so this package compiles and is testable on its
// own; the daemon-side adapter converts.
type Backend interface {
	ListTasks() []process.ProcessInfo
	DaemonStatus() process.DaemonInfo
	ListWorkflows() []WorkflowSummary
	ActiveRuns() []RunSummary
	TriggerWorkflow(name, trigger string) (RunSummary, error)
}

// HistoryReader is the read half of runhistory, declared as an interface
// so handlers can be tested against a stub. *runhistory.Store satisfies it.
type HistoryReader interface {
	RecentTasks(runhistory.Query) ([]runhistory.TaskRecord, error)
	RecentWorkflows(runhistory.Query) ([]runhistory.WorkflowRecord, error)
	WorkflowRun(id string) (runhistory.WorkflowRecord, bool, error)
	StageLogPath(workflow, runID, stage string) string
}

// WorkflowSummary is one declared workflow plus its latest outcome.
type WorkflowSummary struct {
	Key        string    `json:"key"`
	Category   string    `json:"category"`
	Name       string    `json:"name"`
	Cron       string    `json:"cron,omitempty"`
	Stages     []string  `json:"stages"`
	Running    bool      `json:"running"`
	RunID      string    `json:"run_id,omitempty"`
	LastStatus string    `json:"last_status,omitempty"`
	LastRunAt  time.Time `json:"last_run_at,omitzero"`
}

// RunSummary is a run in flight. Finished runs come from the journal
// instead — the journal holds what finished, the daemon holds what is
// running.
type RunSummary struct {
	RunID     string    `json:"run_id"`
	Workflow  string    `json:"workflow"`
	Trigger   string    `json:"trigger"`
	Stage     string    `json:"stage,omitempty"`
	StartedAt time.Time `json:"started_at"`
	Running   bool      `json:"running"`
}
