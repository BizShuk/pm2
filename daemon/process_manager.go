package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/bizshuk/pm2/cron"
	"github.com/bizshuk/pm2/daemon/executor"
	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
	"github.com/fsnotify/fsnotify"
)

// errProcessNotFound is returned by RPC handlers when the target
// resolves to zero managed processes (wrong name, unknown
// namespace, unknown ID, or "all" with an empty registry). Wrapped
// with the target name by processNotFoundError so the user sees
// what they typed.
var errProcessNotFound = errors.New("process or namespace not found")

func processNotFoundError(name string) error {
	return fmt.Errorf("%w: %s", errProcessNotFound, name)
}

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
		return processNotFoundError(name)
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
		return processNotFoundError(name)
	}
	for _, mp := range targets {
		key := cronKey(mp.Info.Namespace, mp.Info.Name)
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
		return processNotFoundError(name)
	}
	for _, mp := range targets {
		_ = pm.stopProcess(mp)
		key := cronKey(mp.Info.Namespace, mp.Info.Name)
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
		return processNotFoundError(name)
	}
	for _, mp := range targets {
		key := cronKey(mp.Info.Namespace, mp.Info.Name)
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
		return processNotFoundError(name)
	}
	for _, mp := range targets {
		_ = pm.stopProcess(mp)
		pm.reg.Remove(cronKey(mp.Info.Namespace, mp.Info.Name))
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
		Version:      model.PM2Version,
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