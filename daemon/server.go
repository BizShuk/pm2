package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"

	_ "github.com/bizshuk/gosdk/log" // initialize gosdk slog default handler

	"github.com/bizshuk/pm2/cron"
	"github.com/bizshuk/pm2/daemon/executor"
	"github.com/bizshuk/pm2/daemon/network"
	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
	"github.com/fsnotify/fsnotify"
)

// Server is the PM2 daemon
type Server struct {
	reg          *ProcessRegistry
	executor     *executor.Executor
	nextID       int
	homeDir      string
	scheduler    *cron.Scheduler
	RestartDelay time.Duration
}

// ManagedProcess pairs runtime state with the OS process handle
type ManagedProcess struct {
	Info     process.ProcessInfo
	Cmd      *exec.Cmd
	done     chan struct{}
	stopping bool // true when deliberately stopped, suppresses auto-restart
	paused   bool // true when suspended via CmdPause; resume re-registers cron
	Watcher  *fsnotify.Watcher
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
func (s *Server) Lock()    { s.reg.Lock() }
func (s *Server) Unlock()  { s.reg.Unlock() }
func (s *Server) RLock()   { s.reg.RLock() }
func (s *Server) RUnlock() { s.reg.RUnlock() }

func NewServer(homeDir string) *Server {
	return &Server{
		reg:       NewProcessRegistry(),
		homeDir:   homeDir,
		executor:  executor.NewExecutor(homeDir),
		scheduler: cron.New(),
		RestartDelay: 30 * time.Second,
	}
}

func (s *Server) startAutoResurrect() {
	if err := s.Resurrect(); err != nil {
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file or directory") {
			slog.Info("auto-resurrect: no saved processes found (dump.json does not exist)")
		} else {
			slog.Error("auto-resurrect failed", "err", err)
		}
	} else {
		slog.Info("auto-resurrect completed successfully")
	}
}

func (s *Server) startAutoSave() {
	intervalStr := os.Getenv("PM2_AUTO_SAVE_INTERVAL")
	interval := 10 * time.Minute
	if intervalStr != "" {
		if d, err := time.ParseDuration(intervalStr); err == nil {
			interval = d
		} else {
			slog.Error("invalid PM2_AUTO_SAVE_INTERVAL", "interval", intervalStr, "err", err)
		}
	}

	slog.Info("auto-save enabled", "interval", interval, "firstRunIn", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		if err := s.Save(); err != nil {
			slog.Error("auto-save failed", "err", err)
		} else {
			slog.Info("auto-save: processes persisted successfully")
		}
	}
}

// Listen starts the Unix socket server.
//
// Phase 5: this is a thin wrapper around network.Listen. The Server
// satisfies the network.Manager interface (see the Manager methods
// declared later in this file), so the network layer can dispatch
// every RPC command to the Server's existing methods without knowing
// about ManagedProcess / ProcessRegistry / Executor.
func (s *Server) Listen(socketPath string) error {
	s.StartMetricsCollector()

	go s.startAutoResurrect()
	go s.startAutoSave()

	return network.Listen(socketPath, s)
}

// StartApp handles the RPC `start` command. It resolves the namespace,
// expands instances to N names, stops any pre-existing process that
// shares (namespace, name, configFile), and launches each one.
//
// Satisfies network.Manager (CmdStart).
func (s *Server) StartApp(req *model.AppStartReq) ([]process.ProcessInfo, error) {
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

		// ProcessRegistry.LookupExistingForLaunch takes the read lock
		// internally and returns both the matching mp and its current
		// map key (the latter needed by launchProcess for cleanup when
		// the old entry was stored under a different namespace).
		existing, _, ok := s.reg.LookupExistingForLaunch(ns, name, req.ConfigFile)
		if ok {
			if existing.Info.Script != req.Script {
				return infos, fmt.Errorf(
					"process %q already exists with script %q; use 'pm2 delete %s' first or use a different name",
					name, existing.Info.Script, name,
				)
			}
			_ = s.stopProcess(existing)
		}

		info, err := s.launchProcess(name, req)
		if err != nil {
			return infos, fmt.Errorf("launch %s: %w", name, err)
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// launchProcess is the Server-side wrapper around executor.Start. It
// owns the registry-state side of a launch (id assignment, mp
// construction, registry write, cron registration) and delegates all
// OS operations to the Executor.
func (s *Server) launchProcess(name string, req *model.AppStartReq) (process.ProcessInfo, error) {
	// Wire the file-watcher onDetect to our restart hook — closure
	// captures `name` so the watcher's debounced callback can locate
	// the right entry. Must be built BEFORE executor.Start so it is
	// captured in the watcher goroutine's closure.
	onFileChanged := func() {
		_ = s.RestartByName(name)
	}

	result, err := s.executor.Start(req, name, onFileChanged)
	if err != nil {
		return process.ProcessInfo{}, fmt.Errorf("executor start: %w", err)
	}

	version := req.Version
	if version == "" {
		version = getAppVersion(req.Script)
	}

	// Lock the registry's write lock so the lookup + ID assignment +
	// map write happen as one atomic operation. This matches the
	// original s.mu.Lock() ... s.mu.Unlock() semantics exactly;
	// ProcessRegistry is now where the lock actually lives.
	s.Lock()
	defer s.Unlock()

	var id int
	var lastCronAt time.Time
	var lastCronStatus string
	var startedAt time.Time
	var pid int
	status := process.StatusOnline

	isCronTask := req.Cron != "" && !req.CronTriggered
	isPaused := req.Paused
	if isPaused {
		// Paused entries come back from save/resurrect carrying the
		// user's deliberate suspension. They are status Paused with no
		// process running and (crucially) NO scheduler entry — that
		// is exactly what makes "paused" different from "idle cron
		// task" (which sits at StatusStopped but is still registered).
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

	existing, oldKey, ok := s.reg.findExistingForLaunchUnderLock(ns, name, req.ConfigFile)

	var restarts int
	if ok {
		id = existing.Info.ID
		lastCronAt = existing.Info.LastCronAt
		lastCronStatus = existing.Info.LastCronStatus
		restarts = existing.Info.Restarts
	} else {
		id = s.nextID
		s.nextID++
	}

	if (req.Cron != "" || req.CronRestart != "") && !isCronTask {
		lastCronAt = startedAt
		lastCronStatus = "ok"
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
				// LogFile / ErrorFile hold the resolved absolute paths —
				// returned by executor.Start (which runs homedir.Expand).
				LogFile:   result.LogFile,
				ErrorFile: result.ErrFile,
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
	s.reg.processes[ns+":"+name] = mp
	if oldKey != "" && oldKey != ns+":"+name {
		delete(s.reg.processes, oldKey)
	}

	// Watch for process exit in background. The Executor owns the
	// cmd.Wait + file-close + done-close + onExit lifecycle; we only
	// supply the state-update closure. Paused entries have no process,
	// so there is nothing to wait for — skip the goroutine entirely.
	// (A paused cron task also has no process; that path was already
	// handled by the !isCronTask check.)
	if !isCronTask && !isPaused {
		go s.executor.Watch(result.Cmd, result.OutF, result.ErrF, result.Watcher, mp.done, func(waitErr error) {
			s.onProcessExit(mp, waitErr)
		})
	}

	// Mark the registry entry paused. The flag drives `pm2 pause` /
	// `pm2 resume` semantics and must round-trip through save/resurrect
	// — see SnapshotAppConfigs and Resurrect in persistence.go.
	mp.paused = isPaused

	// Register cron restart if configured. The scheduler keys entries
	// by "namespace:name" (composite) so that two processes sharing a
	// name across namespaces don't overwrite each other's cron job.
	// Paused entries deliberately do NOT register: the user suspended
	// this schedule, so resurrecting it active would silently undo
	// `pm2 pause`.
	cronKey := ns + ":" + name
	if req.CronRestart != "" && !isPaused {
		if err := s.scheduler.Register(cronKey, req.CronRestart, func() {
			firedAt := time.Now()
			restartErr := s.RestartByName(cronKey)
			// Write last-run info onto the newly launched process
			// (map was replaced by restartByName).
			cronStatus := "ok"
			if restartErr != nil {
				cronStatus = "failed"
			}
			s.reg.UpdateCronStatus(cronKey, firedAt, cronStatus)
		}); err != nil {
			slog.Info("cron_restart parse error", "name", cronKey, "err", err)
		}
	}

	// Register cron schedule if configured. Same pause-exempt rule as
	// cron_restart above.
	if req.Cron != "" && !isPaused {
		if err := s.scheduler.Register(cronKey, req.Cron, func() {
			s.triggerCron(ns, name, req)
		}); err != nil {
			slog.Info("cron parse error", "name", cronKey, "err", err)
		}
	}

	// Hand back a snapshot of ProcessInfo. We're still inside the write
	// lock from the launchProcess critical section above, so this copy
	// is race-free against Watch's onExit writes to mp.Info fields
	// (PID / Status / Restarts).
	info := mp.Info
	return info, nil
}

// onProcessExit is the callback that runs after executor.Watch observes
// cmd.Wait returning. It mirrors what the old watchProcess did inline:
// updates PID/Status, decides whether to auto-restart, and (if so)
// spawns the restart goroutine after RestartDelay.
//
// Lock-ordering invariant: this callback can race with stopProcess's
// onStopped callback for the registry lock. If stopping=true, that
// means Stop has already (or is about to) set Status=StatusStopped —
// we MUST NOT overwrite it back to errored. The guard below makes the
// "stopped" state deterministic regardless of which goroutine wins the
// race for the lock after close(done).
func (s *Server) onProcessExit(mp *ManagedProcess, waitErr error) {
	key := mp.Info.Namespace + ":" + mp.Info.Name
	shouldRestart := false
	s.reg.UpdateInfo(key, func(mp *ManagedProcess) {
		mp.Info.PID = 0
		if !mp.stopping {
			// Process exited on its own (not via deliberate stop).
			if waitErr != nil {
				mp.Info.Status = process.StatusErrored
			} else {
				mp.Info.Status = process.StatusStopped
			}
		}
		// If stopping=true, stopProcess's onStopped callback has set
		// (or is about to set) Status=StatusStopped. Don't race with
		// it — leave Status as Stop set it.

		// Auto-restart if within max restarts limit and not deliberately stopped
		if !mp.stopping && mp.Info.Status == process.StatusErrored && mp.Info.Restarts < mp.Info.MaxRestarts {
			mp.Info.Restarts++
			mp.Info.Status = process.StatusLaunching
			shouldRestart = true
		}
	})

	// Auto-restart: wait RestartDelay, then verify the same mp instance
	// is still in the registry and not stopping, then re-launch.
	if shouldRestart {
		go func() {
			time.Sleep(s.RestartDelay)

			// Snapshot the AppConfig AND verify identity inside the same
			// UpdateInfo callback so the snapshot is race-free against
			// stopProcess's writes to mp.Info (Status / PID). Without
			// this, the goroutine would read mp.Info.* AFTER the lock
			// is released, racing with stopProcess.
			var (
				appCfg  process.AppConfig
				procNS  string
				procNm  string
			)
			ok := s.reg.UpdateInfo(key, func(current *ManagedProcess) {
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
			_ = procNS // reserved for future multi-key dispatch
			req := &model.AppStartReq{AppConfig: appCfg}
			_, _ = s.launchProcess(procNm, req)
		}()
	}
}

// stopProcess is the Server-side wrapper around executor.Stop. It
// owns the registry-state side of a stop (stopping=true + Status
// update via onStopping, scheduler removal) and delegates the actual
// signal / wait / kill to the Executor.
func (s *Server) stopProcess(mp *ManagedProcess) error {
	key := mp.Info.Namespace + ":" + mp.Info.Name

	// Cancel cron before stopping. The scheduler keys entries by
	// "namespace:name" (see launchProcess), so the Remove key must
	// match — otherwise the cron entry leaks and fires on a dead mp.
	s.scheduler.Remove(key)

	// Close the watcher (if any) before the process goes away so the
	// watcher goroutine cannot race with restartByName after we've
	// decided to stop.
	if mp.Watcher != nil {
		_ = mp.Watcher.Close()
		mp.Watcher = nil
	}

	return s.executor.Stop(
		mp.Cmd,
		mp.done,
		// onStopping: mark stopping=true + Status=StatusStopping under lock
		func() {
			s.reg.UpdateInfo(key, func(mp *ManagedProcess) {
				mp.stopping = true
				mp.Info.Status = process.StatusStopping
			})
		},
		// onStopped: clear PID + Status=StatusStopped under lock
		func() {
			s.reg.UpdateInfo(key, func(mp *ManagedProcess) {
				mp.Info.Status = process.StatusStopped
				mp.Info.PID = 0
			})
		},
	)
}

// KillAll gracefully stops every managed process. Used by the kill command
// before the daemon exits.
//
// Satisfies network.Manager (CmdKill).
func (s *Server) KillAll() {
	for _, mp := range s.findProcesses("all") {
		_ = s.stopProcess(mp)
	}
}

// StopByName stops every process matching `name` (exact name, namespace,
// or numeric ID per FindByTarget rules).
//
// Satisfies network.Manager (CmdStop).
func (s *Server) StopByName(name string) error {
	targets := s.findProcesses(name)
	if len(targets) == 0 {
		return fmt.Errorf("process or namespace not found: %s", name)
	}
	for _, mp := range targets {
		_ = s.stopProcess(mp)
	}
	return nil
}

// RestartByName stops then re-launches every matching process.
//
// Satisfies network.Manager (CmdRestart).
func (s *Server) RestartByName(name string) error {
	targets := s.findProcesses(name)
	if len(targets) == 0 {
		return fmt.Errorf("process or namespace not found: %s", name)
	}
	for _, mp := range targets {
		// Snapshot the AppConfig under the registry lock so the
		// struct copy is race-free against watchProcess's writes
		// to mp.Info (PID / Status / Restarts) and against any
		// concurrent stopProcess call. Without this, the field-by-
		// field reads below race with another goroutine mutating
		// adjacent fields on the same mp.Info struct.
		key := mp.Info.Namespace + ":" + mp.Info.Name
		var (
			appCfg process.AppConfig
			pname  string
		)
		if !s.reg.UpdateInfo(key, func(current *ManagedProcess) {
			appCfg = current.Info.AppConfig
			pname = current.Info.Name
		}) {
			continue // process vanished between findProcesses and UpdateInfo
		}
		req := &model.AppStartReq{
			AppConfig:     appCfg,
			CronTriggered: true,
		}
		_ = s.stopProcess(mp)
		_, _ = s.launchProcess(pname, req)
	}
	return nil
}

// PauseByName suspends every matching process. A paused process has its
// cron schedule removed (it will not fire) and any running instance is
// stopped; its status becomes StatusPaused so callers can distinguish a
// deliberately-suspended cron task from one merely idle between fires.
//
// Satisfies network.Manager (CmdPause).
func (s *Server) PauseByName(name string) error {
	targets := s.findProcesses(name)
	if len(targets) == 0 {
		return fmt.Errorf("process or namespace not found: %s", name)
	}
	for _, mp := range targets {
		// stopProcess removes the scheduler entry, sets stopping=true
		// (so watchProcess won't auto-restart), and stops any running
		// instance — leaving status at StatusStopped. We then upgrade
		// that to StatusPaused and mark the process for resume.
		_ = s.stopProcess(mp)
		key := mp.Info.Namespace + ":" + mp.Info.Name
		s.reg.UpdateInfo(key, func(mp *ManagedProcess) {
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
func (s *Server) ResumeByName(name string) error {
	targets := s.findProcesses(name)
	if len(targets) == 0 {
		return fmt.Errorf("process or namespace not found: %s", name)
	}
	for _, mp := range targets {
		key := mp.Info.Namespace + ":" + mp.Info.Name
		paused := false
		s.reg.UpdateInfo(key, func(mp *ManagedProcess) {
			paused = mp.paused
		})
		if !paused {
			continue
		}
		req := &model.AppStartReq{
			AppConfig: process.AppConfig{
				Namespace:   mp.Info.Namespace,
				Name:        mp.Info.Name,
				Script:      mp.Info.Script,
				Args:        mp.Info.Args,
				Env:         mp.Info.Env,
				CronRestart: mp.Info.CronRestart,
				Cron:        mp.Info.Cron,
				Watch:       mp.Info.Watch,
				MaxRestarts: mp.Info.MaxRestarts,
				LogFile:     mp.Info.LogFile,
				ErrorFile:   mp.Info.ErrorFile,
				Version:     mp.Info.Version,
				ConfigFile:  mp.Info.ConfigFile,
				CWD:         mp.Info.CWD,
				BaseEnv:     mp.Info.BaseEnv,
			},
			// CronTriggered stays false: a resumed cron task must go
			// back to scheduled-and-idle, NOT fire immediately.
		}
		if _, err := s.launchProcess(mp.Info.Name, req); err != nil {
			return fmt.Errorf("resume %s: %w", mp.Info.Name, err)
		}
	}
	return nil
}

func (s *Server) triggerCron(ns, name string, originalReq *model.AppStartReq) {
	key := ns + ":" + name
	mp, ok := s.reg.Get(key)
	if !ok {
		return
	}

	triggerReq := *originalReq
	triggerReq.CronTriggered = true

	firedAt := time.Now()

	_ = s.stopProcess(mp)
	_, err := s.launchProcess(name, &triggerReq)

	cronStatus := "ok"
	if err != nil {
		cronStatus = "failed"
	}
	s.reg.UpdateCronStatus(key, firedAt, cronStatus)
}

// StartMetricsCollector spawns a goroutine that runs the executor's
// MetricsCollector refresh loop on a 2-second ticker. Runs until the
// daemon exits.
//
// Uses context.Background() because the daemon does not own a Context
// today (CmdKill triggers os.Exit directly). If a real context is
// added later (signal-driven shutdown), wire it here.
func (s *Server) StartMetricsCollector() {
	collector := executor.NewMetricsCollector(s.reg, executor.MetricsWorkers)
	go collector.Run(context.Background())
}

// refreshMetrics is the test-compatible shim around MetricsCollector.
// It exists so that tests calling s.refreshMetrics() directly (to
// trigger a synchronous refresh) keep working after Phase 4 — the
// actual algorithm now lives in executor.MetricsCollector.Refresh.
func (s *Server) refreshMetrics() {
	executor.NewMetricsCollector(s.reg, executor.MetricsWorkers).Refresh()
}