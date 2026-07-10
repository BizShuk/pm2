package daemon

import (
	"time"

	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
)

// onProcessExit is the callback that runs after executor.Watch observes
// cmd.Wait returning.
func (pm *ProcessManager) onProcessExit(mp *ManagedProcess, waitErr error) {
	key := cronKey(mp.Info.Namespace, mp.Info.Name)
	shouldRestart := false
	pm.reg.UpdateInfo(key, func(mp *ManagedProcess) {
		mp.Info.PID = 0
		if !mp.stopping {
			if waitErr != nil {
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
	})

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
			_, _ = pm.launchProcess(procNm, req)
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
				mp.Info.Status = process.StatusStopping
			})
		},
		func() {
			pm.reg.UpdateInfo(key, func(mp *ManagedProcess) {
				mp.Info.Status = process.StatusStopped
				mp.Info.PID = 0
			})
		},
	)
}

func (pm *ProcessManager) triggerCron(ns, name string, originalReq *model.AppStartReq) {
	key := cronKey(ns, name)
	mp, ok := pm.reg.Get(key)
	if !ok {
		return
	}

	triggerReq := *originalReq
	triggerReq.CronTriggered = true

	firedAt := time.Now()

	_ = pm.stopProcess(mp)
	_, err := pm.launchProcess(name, &triggerReq)

	cronStatus := "ok"
	if err != nil {
		cronStatus = "failed"
	}
	pm.reg.UpdateCronStatus(key, firedAt, cronStatus)
}
