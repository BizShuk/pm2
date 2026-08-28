package wfengine

import (
	"fmt"
	"log/slog"

	"github.com/bizshuk/pm2/runhistory"
	"github.com/bizshuk/pm2/workflow"
)

// armAll re-registers every schedule from scratch and returns warnings
// for the expressions the scheduler rejected.
//
// Register is remove-then-add, so re-arming the whole set is idempotent
// and avoids having to diff the old definitions against the new. A bad
// cron expression is a warning, not a failure, matching how a bad
// cron_restart on an app is logged rather than failing the launch.
func (e *Engine) armAll(defs map[string]workflow.Config) []string {
	var warnings []string

	for _, key := range workflow.Keys(defs) {
		cfg := defs[key]
		if cfg.Cron == "" {
			e.scheduler.Remove(key)
			continue
		}
		if err := e.scheduler.Register(key, cfg.Cron, e.cronFire(key)); err != nil {
			warnings = append(warnings, fmt.Sprintf("workflow %q: cron %q rejected: %v", key, cfg.Cron, err))
			slog.Info("workflow cron parse error", "workflow", key, "cron", cfg.Cron, "err", err)
		}
	}

	for _, ref := range workflow.DanglingRefs(defs) {
		warnings = append(warnings, fmt.Sprintf(
			"workflow %q stage %d references %q, which is not registered yet",
			ref.Workflow, ref.Stage, ref.Target))
	}
	return warnings
}

// cronFire is the scheduled trigger. It treats a run already in flight
// as normal and records a skip, exactly as a cron task's overlap guard
// does: a workflow that runs longer than its interval should run late,
// never be truncated and restarted from its first stage.
//
// An explicit trigger — the CLI or the webhook — gets the error instead,
// because somebody is waiting for an answer.
func (e *Engine) cronFire(key string) func() {
	return func() {
		if _, err := e.Run(key, runhistory.TriggerCron, false); err != nil {
			slog.Info("workflow cron fire skipped", "workflow", key, "err", err)
			e.recordSkip(key, err)
		}
	}
}

// recordSkip journals a fire that produced no run. Without it, "the
// schedule fired and was dropped" and "the schedule never fired" leave
// identical traces.
func (e *Engine) recordSkip(key string, cause error) {
	if e.history == nil {
		return
	}
	category, name := workflow.ParseKey(key)
	now := timeNow()
	rec := runhistory.WorkflowRecord{
		RunID:      runhistory.NewRunID(now),
		Workflow:   key,
		Category:   category,
		Name:       name,
		Trigger:    runhistory.TriggerCron,
		StartedAt:  now,
		FinishedAt: now,
		Status:     runhistory.StatusSkipped,
		Error:      cause.Error(),
	}
	if err := e.history.AppendWorkflow(rec); err != nil {
		slog.Error("workflow skip record failed", "workflow", key, "err", err)
	}
}
