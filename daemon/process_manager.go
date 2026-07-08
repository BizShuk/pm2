package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"time"

	"github.com/bizshuk/pm2/cron"
	"github.com/bizshuk/pm2/daemon/executor"
	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
	"github.com/fsnotify/fsnotify"
)

// ManagedProcess pairs runtime state with the OS process handle.
type ManagedProcess struct {
	Info     process.ProcessInfo
	Cmd      *exec.Cmd
	done     chan struct{}
	stopping bool // true when deliberately stopped, suppresses auto-restart
	paused   bool // true when suspended via CmdPause; resume re-registers cron
	Watcher  *fsnotify.Watcher
}

// ProcessManager owns the core process coordination and control logic:
// process registry, lifecycle management, scheduler management, and
// persistence. It implements network.Manager so the network layer can
// dispatch RPC commands directly without knowing about Server.
type ProcessManager struct {
	reg          *ProcessRegistry
	executor     *executor.Executor
	nextID       int
	homeDir      string
	scheduler    *cron.Scheduler
	startedAt    time.Time
	RestartDelay time.Duration
}

// NewProcessManager returns an initialized ProcessManager ready to
// manage processes. It does not bind any network listener — that is
// the caller's responsibility (typically via Server.Listen).
func NewProcessManager(homeDir string) *ProcessManager {
	return &ProcessManager{
		reg:          NewProcessRegistry(),
		homeDir:      homeDir,
		executor:     executor.NewExecutor(homeDir),
		scheduler:    cron.New(),
		startedAt:    time.Now(),
		RestartDelay: 30 * time.Second,
	}
}

// Lock / Unlock / RLock / RUnlock are escape-hatch delegates to the
// ProcessRegistry's internal RWMutex. They exist so that legacy
// call-sites (and a few internal hot paths) that need to hold the
// registry's lock across more than one operation can do so without
// re-implementing locking. They are NOT a substitute for the high-level
// methods on ProcessRegistry — always prefer Get/Add/UpdateInfo for
// one-shot operations.
//
// IMPORTANT: do not call any ProcessRegistry method while holding these
// locks; sync.RWMutex does not support recursive Lock and recursive
// RLock only works in the no-pending-writer case.
func (pm *ProcessManager) Lock()    { pm.reg.Lock() }
func (pm *ProcessManager) Unlock()  { pm.reg.Unlock() }
func (pm *ProcessManager) RLock()   { pm.reg.RLock() }
func (pm *ProcessManager) RUnlock() { pm.reg.RUnlock() }

// ---------------------------------------------------------------------------
// network.Manager interface implementation
// ---------------------------------------------------------------------------

// StartApp handles the RPC `start` command. It resolves the namespace,
// expands instances to N names, stops any pre-existing process that
// shares (namespace, name, configFile), and launches each one.
//
// Satisfies network.Manager (CmdStart).
func (pm *ProcessManager) StartApp(req *model.AppStartReq) ([]process.ProcessInfo, error) {
	var infos []process.ProcessInfo
	instances := req.Instances
	if instances <= 0 {
		instances = 1
	}

	ns := req.Namespace
	if ns == "" {
		ns = "default"
	}

	for i := 0; i < instances; i++ {
		name := req.Name
		if instances > 1 {
			name = fmt.Sprintf("%s-%d", req.Name, i)
		}

		existing, _, ok := pm.reg.LookupExistingForLaunch(ns, name, req.ConfigFile)
		if ok {
			if existing.Info.Script != req.Script {
				return infos, fmt.Errorf(
					"process %q already exists with script %q; use 'pm2 delete %s' first or use a different name",
					name, existing.Info.Script, name,
				)
			}
			_ = pm.stopProcess(existing)
		}

		info, err := pm.launchProcess(name, req)
		if err != nil {
			return infos, fmt.Errorf("launch %s: %w", name, err)
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// StopByName stops every process matching `name` (exact name, namespace,
// or numeric ID per FindByTarget rules).
//
// Satisfies network.Manager (CmdStop).
func (pm *ProcessManager) StopByName(name string) error {
	targets := pm.findProcesses(name)
	if len(targets) == 0 {
		return fmt.Errorf("process or namespace not found: %s", name)
	}
	for _, mp := range targets {
		_ = pm.stopProcess(mp)
	}
	return nil
}

// RestartByName stops then re-launches every matching process.
//
// Satisfies network.Manager (CmdRestart).
func (pm *ProcessManager) RestartByName(name string) error {
	targets := pm.findProcesses(name)
	if len(targets) == 0 {
		return fmt.Errorf("process or namespace not found: %s", name)
	}
	for _, mp := range targets {
		key := mp.Info.Namespace + ":" + mp.Info.Name
		var (
			appCfg process.AppConfig
			pname  string
		)
		if !pm.reg.UpdateInfo(key, func(current *ManagedProcess) {
			appCfg = current.Info.AppConfig
			pname = current.Info.Name
		}) {
			continue
		}
		req := &model.AppStartReq{
			AppConfig:     appCfg,
			CronTriggered: true,
		}
		_ = pm.stopProcess(mp)
		_, _ = pm.launchProcess(pname, req)
	}
	return nil
}

// PauseByName suspends every matching process. A paused process has its
// cron schedule removed (it will not fire) and any running instance is
// stopped; its status becomes StatusPaused so callers can distinguish a
// deliberately-suspended cron task from one merely idle between fires.
//
// Satisfies network.Manager (CmdPause).
func (pm *ProcessManager) PauseByName(name string) error {
	targets := pm.findProcesses(name)
	if len(targets) == 0 {
		return fmt.Errorf("process or namespace not found: %s", name)
	}
	for _, mp := range targets {
		_ = pm.stopProcess(mp)
		key := mp.Info.Namespace + ":" + mp.Info.Name
		pm.reg.UpdateInfo(key, func(mp *ManagedProcess) {
			mp.paused = true
			mp.Info.Status = process.StatusPaused
		})
	}
	return nil
}

// ResumeByName reverses PauseByName for every matching process. It
// re-launches through launchProcess, which re-registers the cron
// schedule and returns a cron task to its idle StatusStopped state (or
// a regular process to StatusOnline). Resuming a process that was not
// paused is a no-op that still succeeds, keeping the command idempotent.
//
// Satisfies network.Manager (CmdResume).
func (pm *ProcessManager) ResumeByName(name string) error {
	targets := pm.findProcesses(name)
	if len(targets) == 0 {
		return fmt.Errorf("process or namespace not found: %s", name)
	}
	for _, mp := range targets {
		key := mp.Info.Namespace + ":" + mp.Info.Name
		var (
			paused bool
			appCfg process.AppConfig
		)
		pm.reg.UpdateInfo(key, func(current *ManagedProcess) {
			paused = current.paused
			appCfg = current.Info.AppConfig
		})
		if !paused {
			continue
		}
		req := &model.AppStartReq{AppConfig: appCfg}
		if _, err := pm.launchProcess(appCfg.Name, req); err != nil {
			return fmt.Errorf("resume %s: %w", appCfg.Name, err)
		}
	}
	return nil
}

// DeleteByName removes every matching process from the registry.
// The process is stopped first; the registry entry is then removed.
//
// Satisfies network.Manager (CmdDelete).
func (pm *ProcessManager) DeleteByName(name string) error {
	targets := pm.findProcesses(name)
	if len(targets) == 0 {
		return fmt.Errorf("process or namespace not found: %s", name)
	}
	for _, mp := range targets {
		_ = pm.stopProcess(mp)
		pm.reg.Remove(mp.Info.Namespace + ":" + mp.Info.Name)
	}
	return nil
}

// ListAll returns a snapshot of every managed process's ProcessInfo.
//
// Satisfies network.Manager (CmdList).
func (pm *ProcessManager) ListAll() []process.ProcessInfo {
	return pm.reg.Snapshot()
}

// Save serialises the in-memory process map to <homeDir>/dump.json.
//
// Satisfies network.Manager (CmdSave).
func (pm *ProcessManager) Save() error {
	entries := pm.reg.SnapshotAppConfigs()

	dumpPath := filepath.Join(pm.homeDir, "dump.json")
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dumpPath, data, 0o644)
}

// Resurrect reads <homeDir>/dump.json and starts every saved process.
// A per-entry failure is logged but does not abort the rest.
//
// Satisfies network.Manager (CmdResurrect).
func (pm *ProcessManager) Resurrect() error {
	dumpPath := filepath.Join(pm.homeDir, "dump.json")
	data, err := os.ReadFile(dumpPath)
	if err != nil {
		return fmt.Errorf("no dump found (run pm2 save first): %w", err)
	}
	var entries []process.AppConfig
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("dump.json format incompatible (unified-config refactor — please run `pm2 delete all` then re-add your apps, or restore from a pre-refactor backup): %w", err)
	}
	for i := range entries {
		entries[i].Normalize("")
		req := &model.AppStartReq{AppConfig: entries[i]}
		req.Paused = entries[i].Paused
		if _, err := pm.StartApp(req); err != nil {
			slog.Info("resurrect error", "name", entries[i].Name, "err", err)
		}
	}
	return nil
}

// KillAll gracefully stops every managed process. Used by the kill command
// before the daemon exits.
//
// Satisfies network.Manager (CmdKill).
func (pm *ProcessManager) KillAll() {
	for _, mp := range pm.findProcesses("all") {
		_ = pm.stopProcess(mp)
	}
}

// Ping is the CmdPing handler.
//
// Satisfies network.Manager (CmdPing).
func (pm *ProcessManager) Ping() {
	// No-op for now — the dispatcher returns OK without inspecting any
	// state. Override behavior here if a richer health check is added.
}

// Status returns a snapshot of the daemon's identity + light runtime
// counters.
//
// Satisfies network.Manager (CmdStatus).
func (pm *ProcessManager) Status() process.DaemonInfo {
	return process.DaemonInfo{
		PID:          os.Getpid(),
		StartedAt:    pm.startedAt,
		Version:      PM2Version,
		HomeDir:      pm.homeDir,
		ProcessCount: pm.reg.Len(),
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// findProcesses resolves a target string to one or more managed processes.
func (pm *ProcessManager) findProcesses(target string) []*ManagedProcess {
	return pm.reg.FindByTarget(target)
}

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
	pm.reg.processes[ns+":"+name] = mp
	if oldKey != "" && oldKey != ns+":"+name {
		delete(pm.reg.processes, oldKey)
	}

	if !isCronTask && !isPaused {
		go pm.executor.Watch(result.Cmd, result.OutF, result.ErrF, result.Watcher, mp.done, func(waitErr error) {
			pm.onProcessExit(mp, waitErr)
		})
	}

	mp.paused = isPaused

	cronKey := ns + ":" + name
	if req.CronRestart != "" && !isPaused {
		if err := pm.scheduler.Register(cronKey, req.CronRestart, func() {
			firedAt := time.Now()
			restartErr := pm.RestartByName(cronKey)
			cronStatus := "ok"
			if restartErr != nil {
				cronStatus = "failed"
			}
			pm.reg.UpdateCronStatus(cronKey, firedAt, cronStatus)
		}); err != nil {
			slog.Info("cron_restart parse error", "name", cronKey, "err", err)
		}
	}

	if req.Cron != "" && !isPaused {
		if err := pm.scheduler.Register(cronKey, req.Cron, func() {
			pm.triggerCron(ns, name, req)
		}); err != nil {
			slog.Info("cron parse error", "name", cronKey, "err", err)
		}
	}

	info := mp.Info
	return info, nil
}

// onProcessExit is the callback that runs after executor.Watch observes
// cmd.Wait returning.
func (pm *ProcessManager) onProcessExit(mp *ManagedProcess, waitErr error) {
	key := mp.Info.Namespace + ":" + mp.Info.Name
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
	key := mp.Info.Namespace + ":" + mp.Info.Name

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
	key := ns + ":" + name
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

// ---------------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------------

// StartMetricsCollector spawns a goroutine that runs the executor's
// MetricsCollector refresh loop on a 2-second ticker.
func (pm *ProcessManager) StartMetricsCollector() {
	collector := executor.NewMetricsCollector(pm.reg, executor.MetricsWorkers)
	go collector.Run(context.Background())
}

// refreshMetrics is the test-compatible shim around MetricsCollector.
func (pm *ProcessManager) refreshMetrics() {
	executor.NewMetricsCollector(pm.reg, executor.MetricsWorkers).Refresh()
}