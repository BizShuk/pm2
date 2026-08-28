package web

import (
	"time"

	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/runhistory"
)

const (
	// DefaultHost is every interface, so other machines on the local
	// network can open the dashboard. There is deliberately no tunnel and
	// no internet exposure: the LAN is the boundary.
	//
	// Note the deviation this records. The workspace port rule reads
	// "LAN reachable -> public segment" and "internal -> 127.0.0.1
	// prefix"; this service is an admin console numbered internal but
	// bound LAN-wide. That is an explicit choice, and the reason the
	// same-origin guard in guard.go is load-bearing rather than
	// decorative — the bind is not doing the work here.
	DefaultHost = "0.0.0.0"

	// DefaultPort sits in the workspace's internal segment (8500-8599),
	// taking its smallest unused number. This is an admin interface, not
	// a product surface.
	DefaultPort = 8502
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
