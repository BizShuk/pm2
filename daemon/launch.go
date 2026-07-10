package daemon

import (
	"fmt"
	"log/slog"
	"os/user"
	"time"

	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
)

// launchProcess is the ProcessManager-side wrapper around executor.Start. It
// owns the registry-state side of a launch (id assignment, mp
// construction, registry write, cron registration) and delegates all
// OS operations to the Executor.
func (pm *ProcessManager) launchProcess(name string, req *model.AppStartReq) (process.ProcessInfo, error) {
	onFileChanged := func() {
		_ = pm.RestartByName(name)
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
		ns = "default"
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
				ConfigDir:   req.ConfigDir,
				Version:     version,
				Watch:       req.Watch,
				ConfigFile:  req.ConfigFile,
				CWD:         req.CWD,
				BaseEnv:     req.BaseEnv,
				LogFile:     result.LogFile,
				ErrorFile:   result.ErrFile,
			},
			ID:             id,
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
	}
	pm.reg.processes[cronKey(ns, name)] = mp
	if oldKey != "" && oldKey != cronKey(ns, name) {
		delete(pm.reg.processes, oldKey)
	}

	if !isCronTask && !isPaused {
		go pm.executor.Watch(result.Cmd, result.OutF, result.ErrF, result.Watcher, mp.done, func(waitErr error) {
			pm.onProcessExit(mp, waitErr)
		})
	}

	mp.paused = isPaused

	ck := cronKey(ns, name)
	if req.CronRestart != "" && !isPaused {
		if err := pm.scheduler.Register(ck, req.CronRestart, func() {
			firedAt := time.Now()
			restartErr := pm.RestartByName(ck)
			cronStatus := "ok"
			if restartErr != nil {
				cronStatus = "failed"
			}
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
