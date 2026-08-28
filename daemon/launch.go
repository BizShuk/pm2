package daemon

import (
	"fmt"
	"log/slog"
	"os/user"
	"time"

	"github.com/bizshuk/pm2/daemon/executor"
	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/runhistory"
)

// launchProcess launches a process the user asked for directly.
func (pm *ProcessManager) launchProcess(name string, req *model.AppStartReq) (process.ProcessInfo, error) {
	return pm.launchProcessWith(name, req, runhistory.TriggerManual)
}

// launchProcessWith is the ProcessManager-side wrapper around
// executor.Start. It owns the registry-state side of a launch (id
// assignment, mp construction, registry write, cron registration) and
// delegates all OS operations to the Executor.
//
// trigger records why this run started. It is stamped onto the entry
// under the same write lock that installs it, because onProcessExit —
// which is where the run journal is written — has no other way to know.
func (pm *ProcessManager) launchProcessWith(name string, req *model.AppStartReq, trigger string) (process.ProcessInfo, error) {
	onFileChanged := func() {
		// restartTargets, not RestartByName: a watch-triggered restart is
		// not a user operation and changes nothing that dump.json stores.
		_ = pm.restartTargets(name, runhistory.TriggerWatch)
	}

	result, err := pm.executor.Start(req, name, onFileChanged)
	if err != nil {
		return process.ProcessInfo{}, fmt.Errorf("executor start: %w", err)
	}

	version := req.Version
	if version == "" {
		version = getAppVersion(req.Script)
	}

	pm.Lock()
	defer pm.Unlock()

	var id int
	var lastCronAt time.Time
	var lastCronStatus string
	var startedAt time.Time
	var pid int
	status := process.StatusOnline

	isCronTask := req.Cron != "" && !req.CronTriggered
	isPaused := req.Paused
	if isPaused {
		status = process.StatusPaused
	} else if isCronTask {
		status = process.StatusStopped
	} else {
		startedAt = time.Now()
		pid = result.Cmd.Process.Pid
	}

	ns := req.Namespace
	if ns == "" {
		ns = process.DefaultNamespace
	}

	existing, oldKey, ok := pm.reg.findExistingForLaunchUnderLock(ns, name, req.ConfigFile)

	if ok && existing.paused && req.CronTriggered {
		info := existing.Info
		go pm.executor.Watch(result.Cmd, result.OutF, result.ErrF, result.Watcher, nil, nil)
		return info, nil
	}

	var restarts int
	if ok {
		id = existing.Info.ID
		lastCronAt = existing.Info.LastCronAt
		lastCronStatus = existing.Info.LastCronStatus
		restarts = existing.Info.Restarts
	} else {
		id = pm.nextID
		pm.nextID++
	}

	currentUser := "unknown"
	if u, err := user.Current(); err == nil {
		currentUser = u.Username
	}

	mp := &ManagedProcess{
		Info: process.ProcessInfo{
			AppConfig: process.AppConfig{
				Namespace:   ns,
				Name:        name,
				Script:      req.Script,
				Args:        req.Args,
				Env:         req.Env,
				CronRestart: req.CronRestart,
				Cron:        req.Cron,
				MaxRestarts: req.MaxRestarts,
				Version:     version,
				Watch:       req.Watch,
				ConfigFile:  req.ConfigFile,
				CWD:         result.CWD,
				BaseEnv:     req.BaseEnv,
				Optional:    req.Optional,
			},
			ID:             id,
			LogFile:        result.LogFile,
			ErrorFile:      result.ErrFile,
			PID:            pid,
			Status:         status,
			StartedAt:      startedAt,
			Restarts:       restarts,
			User:           currentUser,
			LastCronAt:     lastCronAt,
			LastCronStatus: lastCronStatus,
		},
		Cmd:     result.Cmd,
		done:    make(chan struct{}),
		Watcher: result.Watcher,
		runID:   runhistory.NewRunID(time.Now()),
		trigger: trigger,
	}
	pm.reg.processes[cronKey(ns, name)] = mp
	if oldKey != "" && oldKey != cronKey(ns, name) {
		delete(pm.reg.processes, oldKey)
	}

	if !isCronTask && !isPaused {
		go pm.executor.Watch(result.Cmd, result.OutF, result.ErrF, result.Watcher, mp.done, func(exit executor.ExitInfo) {
			pm.onProcessExit(mp, exit)
		})
	}

	mp.paused = isPaused

	ck := cronKey(ns, name)
	if req.CronRestart != "" && !isPaused {
		if err := pm.scheduler.Register(ck, req.CronRestart, func() {
			firedAt := time.Now()
			// restartTargets: a cron fire persists nothing new, so it must
			// not rewrite dump.json on every tick.
			restartErr := pm.restartTargets(ck, runhistory.TriggerCronRestart)
			cronStatus := "ok"
			if restartErr != nil {
				cronStatus = "failed"
				pm.recordLaunchFailure(ns, name, runhistory.TriggerCronRestart, restartErr)
			}
			// Unlike a one-shot cron task, cron_restart reboots a
			// long-lived process: "the child is still running" is its
			// normal state, so there is no later exit to wait for and
			// "ok" already means what it says.
			pm.reg.UpdateCronStatus(ck, firedAt, cronStatus)
		}); err != nil {
			slog.Info("cron_restart parse error", "name", ck, "err", err)
		}
	}

	if req.Cron != "" && !isPaused {
		if err := pm.scheduler.Register(ck, req.Cron, func() {
			pm.triggerCron(ns, name, req)
		}); err != nil {
			slog.Info("cron parse error", "name", ck, "err", err)
		}
	}

	info := mp.Info
	return info, nil
}
