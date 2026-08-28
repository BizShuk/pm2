package daemon

import (
	"log/slog"
	"sync"
	"time"

	"github.com/bizshuk/pm2/daemon/executor"
	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/runhistory"
)

// This file is the only place in the daemon that knows the run journal
// exists, mirroring autosave.go's role for dump.json — and it borrows
// that file's contract exactly: every append is best-effort, failures
// are logged and never returned. The run already happened to a real
// process; failing an RPC because a journal line could not be written
// would misreport what actually occurred.
//
// The invariant the call sites implement: one record per *finished*
// run, plus a record for a fire that produced no run at all. A task
// that starts is not journaled — the daemon reports what is running,
// the journal reports what finished. Recording both would double the
// volume of a per-minute cron task and force every reader to join.

// historyLogInterval rate-limits the failure log. A full disk fails
// every append, and without this the daemon's own log becomes a copy of
// the journal it could not write.
const historyLogInterval = time.Minute

var historyLogGate struct {
	mu   sync.Mutex
	last time.Time
}

func logHistoryFailure(err error, attrs ...any) {
	historyLogGate.mu.Lock()
	now := time.Now()
	quiet := now.Sub(historyLogGate.last) < historyLogInterval
	if !quiet {
		historyLogGate.last = now
	}
	historyLogGate.mu.Unlock()

	if quiet {
		return
	}
	slog.Error("run history append failed", append(attrs, "err", err)...)
}

// recordRun journals a task run that has finished. It is called from
// onProcessExit, the single point every managed process passes through
// on its way out, whatever started it.
func (pm *ProcessManager) recordRun(info process.ProcessInfo, runID, trigger string, exit executor.ExitInfo) {
	if pm.history == nil {
		return
	}

	now := time.Now()
	status := runhistory.StatusFailed
	if exit.Success() {
		status = runhistory.StatusSuccess
	}

	err := pm.history.AppendTask(runhistory.TaskRecord{
		TS:         now,
		Event:      runhistory.EventRun,
		RunID:      runID,
		Namespace:  info.Namespace,
		Name:       info.Name,
		ID:         info.ID,
		Trigger:    trigger,
		PID:        info.PID,
		StartedAt:  info.StartedAt,
		DurationMS: sinceMS(info.StartedAt, now),
		ExitCode:   runhistory.ExitCodeOf(exit.Code, exit.Known),
		Signal:     exit.Signal,
		Restarts:   info.Restarts,
		Status:     status,
	})
	if err != nil {
		logHistoryFailure(err, "event", runhistory.EventRun, "task", cronKey(info.Namespace, info.Name))
	}
}

// recordCronSkip journals a fire dropped because the previous run had
// not finished. It produces no run, so nothing else would record it,
// and "the schedule fired and was skipped" must stay distinguishable
// from "the schedule never fired".
func (pm *ProcessManager) recordCronSkip(namespace, name string, firedAt time.Time) {
	if pm.history == nil {
		return
	}
	err := pm.history.AppendTask(runhistory.TaskRecord{
		TS:        firedAt,
		Event:     runhistory.EventCronSkip,
		RunID:     runhistory.NewRunID(firedAt),
		Namespace: namespace,
		Name:      name,
		Trigger:   runhistory.TriggerCron,
		Status:    runhistory.StatusSkipped,
		Detail:    "previous run still active",
	})
	if err != nil {
		logHistoryFailure(err, "event", runhistory.EventCronSkip, "task", cronKey(namespace, name))
	}
}

// recordLaunchFailure journals a fire that never produced a child. Its
// ExitCode is nil rather than 0 — there was no process to exit.
func (pm *ProcessManager) recordLaunchFailure(namespace, name, trigger string, cause error) {
	if pm.history == nil {
		return
	}
	detail := ""
	if cause != nil {
		detail = cause.Error()
	}
	err := pm.history.AppendTask(runhistory.TaskRecord{
		TS:        time.Now(),
		Event:     runhistory.EventLaunchFail,
		RunID:     runhistory.NewRunID(time.Now()),
		Namespace: namespace,
		Name:      name,
		Trigger:   trigger,
		Status:    runhistory.StatusFailed,
		Detail:    detail,
	})
	if err != nil {
		logHistoryFailure(err, "event", runhistory.EventLaunchFail, "task", cronKey(namespace, name))
	}
}

// pruneHistory drops journal day files outside the retention window.
// Called once at daemon start; appends handle the rest by pruning
// whenever they roll over to a new day.
func (pm *ProcessManager) pruneHistory() {
	if pm.history == nil {
		return
	}
	if err := pm.history.Prune(runhistory.DefaultKeepDays); err != nil {
		slog.Error("run history prune failed", "err", err, "homeDir", pm.homeDir)
	}
}

func sinceMS(start, end time.Time) int64 {
	if start.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}
