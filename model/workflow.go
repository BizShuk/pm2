package model

import (
	"github.com/bizshuk/pm2/runhistory"
	"github.com/bizshuk/pm2/workflow"
)

// WorkflowReq is the payload for every workflow command, mirroring how
// App carries the payload for CmdStart.
//
// Which fields matter depends on the command: Configs for register,
// Ref for run and delete, RunID for stop, Wait for run.
type WorkflowReq struct {
	Configs []workflow.Config `json:"configs,omitempty"`
	Ref     string            `json:"ref,omitempty"`
	RunID   string            `json:"run_id,omitempty"`
	Wait    bool              `json:"wait,omitempty"`
}

// Trigger is always "manual" for a request that arrived over the socket.
//
// It is deliberately not a field: cron and webhook triggers originate
// inside the daemon, and letting a CLI name its own trigger would let it
// forge history. The web layer calls the manager directly and supplies
// its own.
func (r *WorkflowReq) Trigger() string { return runhistory.TriggerManual }

// RegisterResult is the CmdWorkflowRegister payload.
//
// Warnings are non-fatal findings the CLI prints: a stage referencing a
// workflow that is not registered yet, or a cron expression the
// scheduler rejected. A declared cycle is not among them — that fails
// the whole batch.
type RegisterResult struct {
	Registered []string `json:"registered"`
	Warnings   []string `json:"warnings,omitempty"`
}
