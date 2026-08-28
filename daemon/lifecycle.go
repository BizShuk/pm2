package daemon

import (
	"log/slog"
	"time"

	"github.com/bizshuk/pm2/daemon/executor"
	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/runhistory"
)

// onProcessExit is the callback that runs after executor.Watch observes
// cmd.Wait returning.
func (pm *ProcessManager) onProcessExit(mp *ManagedProcess, exit executor.ExitInfo) {
	key := cronKey(mp.Info.Namespace, mp.Info.Name)
	shouldRestart := false

	// Snapshot the run's identity inside the same closure that mutates
	// it. Reading mp.runID / mp.trigger outside the lock would race with
	// a concurrent relaunch, which is the naked-read hazard the registry
	// exists to prevent.
	var (
		snapshot process.ProcessInfo
		runID    string
		trigger  string
	)
	pm.reg.UpdateInfo(key, func(mp *ManagedProcess) {
		mp.Info.PID = 0
		if !mp.stopping {
			if exit.Err != nil {
				mp.Info.Status = process.StatusErrored
			} else {
				mp.Info.Status = process.StatusStopped
			}
		}

		if !mp.stopping && mp.Info.Status == process.StatusErrored && mp.Info.Restarts < mp.Info.MaxRestarts {
			mp.Info.Restarts++
			mp.Info.Status = process.StatusLaunching
			shouldRestart = true
		}

		snapshot, runID, trigger = mp.Info, mp.runID, mp.trigger
	})

	// The single point every managed process passes through on its way
	// out, whatever started it — so the single point the run journal is
	// written from.
	snapshot.PID = pidOf(mp)
	pm.recordRun(snapshot, runID, trigger, exit)

	// A one-shot cron task's outcome is only knowable here. At fire time
	// triggerCron could say no more than "launched", so "ok" used to mean
	// merely that the child had spawned.
	//
	// Only a cron-triggered run may write this field: an ordinary process
	// exiting must not overwrite a status that belongs to the schedule.
	if trigger == runhistory.TriggerCron {
		outcome := "ok"
		if !exit.Success() {
			outcome = "failed"
		}
		pm.reg.UpdateCronOutcome(key, outcome)
	}

	if shouldRestart {
		go func() {
			time.Sleep(pm.RestartDelay)

			var (
				appCfg process.AppConfig
				procNS string
				procNm string
			)
			ok := pm.reg.UpdateInfo(key, func(current *ManagedProcess) {
				if current != mp || current.stopping {
					shouldRestart = false
					return
				}
				appCfg = current.Info.AppConfig
				procNS = current.Info.Namespace
				procNm = current.Info.Name
			})
			if !ok || !shouldRestart {
				return
			}
			_ = procNS
			req := &model.AppStartReq{AppConfig: appCfg}
			_, _ = pm.launchProcessWith(procNm, req, runhistory.TriggerAutoRestart)
		}()
	}
}

// stopProcess is the ProcessManager-side wrapper around executor.Stop.
func (pm *ProcessManager) stopProcess(mp *ManagedProcess) error {
	key := cronKey(mp.Info.Namespace, mp.Info.Name)

	pm.scheduler.Remove(key)

	if mp.Watcher != nil {
		_ = mp.Watcher.Close()
		mp.Watcher = nil
	}

	return pm.executor.Stop(
		mp.Cmd,
		mp.done,
		func() {
			pm.reg.UpdateInfo(key, func(mp *ManagedProcess) {
				mp.stopping = true
				if !mp.paused {
					mp.Info.Status = process.StatusStopping
				}
			})
		},
		func() {
			pm.reg.UpdateInfo(key, func(mp *ManagedProcess) {
				if !mp.paused {
					mp.Info.Status = process.StatusStopped
				}
				mp.Info.PID = 0
			})
		},
	)
}

// CronStatusSkipped is recorded on LastCronStatus when a fire was dropped
// because the previous run had not finished.
const CronStatusSkipped = "skipped"

// CronStatusRunning is recorded between a fire and the child's exit.
// It exists because "ok" must mean "exited 0": at fire time all that is
// known is that the child spawned, and reporting that as success made
// every failing cron task look healthy.
const CronStatusRunning = "running"

func (pm *ProcessManager) triggerCron(ns, name string, originalReq *model.AppStartReq) {
	key := cronKey(ns, name)
	mp, ok := pm.reg.Get(key)
	if !ok {
		return
	}

	firedAt := time.Now()

	// Overlap guard: a fire that arrives while the previous run is still
	// active is dropped, not stacked. The alternative — the stopProcess
	// below — would SIGTERM a job the schedule itself asked for, so a task
	// that occasionally runs longer than its interval would be truncated
	// every time instead of simply running late.
	if info, ok := pm.reg.SnapshotOne(key); ok && cronRunActive(info) {
		slog.Info("cron fire skipped: previous run still active",
			"name", key, "pid", info.PID, "status", info.Status)
		pm.reg.UpdateCronStatus(key, firedAt, CronStatusSkipped)
		pm.recordCronSkip(ns, name, firedAt)
		return
	}

	triggerReq := *originalReq
	triggerReq.CronTriggered = true

	_ = pm.stopProcess(mp)
	_, err := pm.launchProcessWith(name, &triggerReq, runhistory.TriggerCron)

	// "running" — launched, outcome unknown. onProcessExit replaces it
	// with ok/failed once the child actually exits. A launch that never
	// produced a child is terminal here and is journaled, because
	// nothing downstream will ever see it.
	cronStatus := CronStatusRunning
	if err != nil {
		cronStatus = "failed"
		pm.recordLaunchFailure(ns, name, runhistory.TriggerCron, err)
	}
	pm.reg.UpdateCronStatus(key, firedAt, cronStatus)
}

// pidOf reads the live PID off the OS handle rather than the registry
// copy, which onProcessExit has just cleared to 0. The journal wants the
// process that ran, not the absence that followed it.
func pidOf(mp *ManagedProcess) int {
	if mp == nil || mp.Cmd == nil || mp.Cmd.Process == nil {
		return 0
	}
	return mp.Cmd.Process.Pid
}

// cronRunActive reports whether a previous cron fire is still in flight.
// A live PID covers the running and stopping cases; StatusLaunching covers
// the auto-restart window, where onProcessExit has already cleared the PID
// but a replacement child is about to be spawned. An idle cron task between
// fires sits at PID 0 / stopped and is therefore not active.
func cronRunActive(info process.ProcessInfo) bool {
	return info.PID != 0 || info.Status == process.StatusLaunching
}
