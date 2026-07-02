package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"

	_ "github.com/bizshuk/gosdk/log" // initialize gosdk slog default handler

	"github.com/bizshuk/pm2/cron"
	"github.com/bizshuk/pm2/daemon/executor"
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
	if err := s.resurrect(); err != nil {
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
		if err := s.save(); err != nil {
			slog.Error("auto-save failed", "err", err)
		} else {
			slog.Info("auto-save: processes persisted successfully")
		}
	}
}

// Listen starts the Unix socket server
func (s *Server) Listen(socketPath string) error {
	s.StartMetricsCollector()
	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	slog.Info("daemon listening", "socketPath", socketPath)

	go s.startAutoResurrect()
	go s.startAutoSave()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	var req model.Request
	if err := model.ReadJSON(conn, &req); err != nil {
		_ = model.WriteJSON(conn, model.Response{Error: err.Error()})
		return
	}

	var resp model.Response
	switch req.Command {
	case model.CmdPing:
		resp = model.Response{OK: true}

	case model.CmdStart:
		if req.App == nil {
			resp = model.Response{Error: "missing app config"}
		} else {
			infos, err := s.startApp(req.App)
			if err != nil {
				resp = model.Response{Error: err.Error()}
			} else {
				payload, _ := json.Marshal(infos)
				resp = model.Response{OK: true, Payload: payload}
			}
		}

	case model.CmdStop:
		err := s.stopByName(req.Name)
		if err != nil {
			resp = model.Response{Error: err.Error()}
		} else {
			resp = model.Response{OK: true}
		}

	case model.CmdRestart:
		err := s.restartByName(req.Name)
		if err != nil {
			resp = model.Response{Error: err.Error()}
		} else {
			resp = model.Response{OK: true}
		}

	case model.CmdPause:
		err := s.pauseByName(req.Name)
		if err != nil {
			resp = model.Response{Error: err.Error()}
		} else {
			resp = model.Response{OK: true}
		}

	case model.CmdResume:
		err := s.resumeByName(req.Name)
		if err != nil {
			resp = model.Response{Error: err.Error()}
		} else {
			resp = model.Response{OK: true}
		}

	case model.CmdDelete:
		err := s.deleteByName(req.Name)
		if err != nil {
			resp = model.Response{Error: err.Error()}
		} else {
			resp = model.Response{OK: true}
		}

	case model.CmdList:
		infos := s.listAll()
		payload, _ := json.Marshal(infos)
		resp = model.Response{OK: true, Payload: payload}

	case model.CmdSave:
		if err := s.save(); err != nil {
			resp = model.Response{Error: err.Error()}
		} else {
			resp = model.Response{OK: true}
		}

	case model.CmdResurrect:
		if err := s.resurrect(); err != nil {
			resp = model.Response{Error: err.Error()}
		} else {
			resp = model.Response{OK: true}
		}

	case model.CmdKill:
		// Gracefully stop every managed process, reply, then exit the daemon.
		s.killAll()
		resp = model.Response{OK: true}
		// Exit after the response is flushed below; the small delay lets
		// model.WriteJSON at the end of handleConn complete first.
		go func() {
			time.Sleep(150 * time.Millisecond)
			slog.Info("daemon shutting down via kill command")
			os.Exit(0)
		}()

	default:
		resp = model.Response{Error: fmt.Sprintf("unknown command: %s", req.Command)}
	}

	_ = model.WriteJSON(conn, resp)
}

func (s *Server) startApp(req *model.AppStartReq) ([]process.ProcessInfo, error) {
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
		_ = s.restartByName(name)
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
	if isCronTask {
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
	// supply the state-update closure.
	if !isCronTask {
		go s.executor.Watch(result.Cmd, result.OutF, result.ErrF, result.Watcher, mp.done, func(waitErr error) {
			s.onProcessExit(mp, waitErr)
		})
	}

	// Register cron restart if configured. The scheduler keys entries
	// by "namespace:name" (composite) so that two processes sharing a
	// name across namespaces don't overwrite each other's cron job.
	cronKey := ns + ":" + name
	if req.CronRestart != "" {
		if err := s.scheduler.Register(cronKey, req.CronRestart, func() {
			firedAt := time.Now()
			restartErr := s.restartByName(cronKey)
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

	// Register cron schedule if configured
	if req.Cron != "" {
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
func (s *Server) onProcessExit(mp *ManagedProcess, waitErr error) {
	key := mp.Info.Namespace + ":" + mp.Info.Name
	shouldRestart := false
	s.reg.UpdateInfo(key, func(mp *ManagedProcess) {
		mp.Info.PID = 0
		if waitErr != nil {
			mp.Info.Status = process.StatusErrored
		} else {
			mp.Info.Status = process.StatusStopped
		}

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

			ok := s.reg.UpdateInfo(key, func(current *ManagedProcess) {
				if current != mp || current.stopping {
					shouldRestart = false
				}
			})
			if !ok || !shouldRestart {
				return
			}

			req := &model.AppStartReq{
				AppConfig: process.AppConfig{
					Namespace:   mp.Info.Namespace,
					Name:        mp.Info.Name,
					Script:      mp.Info.Script,
					Args:        mp.Info.Args,
					Env:         mp.Info.Env,
					CronRestart: mp.Info.CronRestart,
					Watch:       mp.Info.Watch,
					MaxRestarts: mp.Info.MaxRestarts,
					LogFile:     mp.Info.LogFile,
					ErrorFile:   mp.Info.ErrorFile,
					ConfigFile:  mp.Info.ConfigFile,
					CWD:         mp.Info.CWD,
					BaseEnv:     mp.Info.BaseEnv,
				},
			}
			_, _ = s.launchProcess(mp.Info.Name, req)
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

// killAll gracefully stops every managed process. Used by the kill command
// before the daemon exits.
func (s *Server) killAll() {
	for _, mp := range s.findProcesses("all") {
		_ = s.stopProcess(mp)
	}
}

func (s *Server) stopByName(name string) error {
	targets := s.findProcesses(name)
	if len(targets) == 0 {
		return fmt.Errorf("process or namespace not found: %s", name)
	}
	for _, mp := range targets {
		_ = s.stopProcess(mp)
	}
	return nil
}

func (s *Server) restartByName(name string) error {
	targets := s.findProcesses(name)
	if len(targets) == 0 {
		return fmt.Errorf("process or namespace not found: %s", name)
	}
	for _, mp := range targets {
		req := &model.AppStartReq{
			AppConfig: process.AppConfig{
				Namespace:     mp.Info.Namespace,
				Name:          mp.Info.Name,
				Script:        mp.Info.Script,
				Args:          mp.Info.Args,
				Env:           mp.Info.Env,
				CronRestart:   mp.Info.CronRestart,
				Cron:          mp.Info.Cron,
				Watch:         mp.Info.Watch,
				MaxRestarts:   mp.Info.MaxRestarts,
				LogFile:       mp.Info.LogFile,
				ErrorFile:     mp.Info.ErrorFile,
				Version:       mp.Info.Version,
				ConfigFile:    mp.Info.ConfigFile,
				CWD:           mp.Info.CWD,
				BaseEnv:       mp.Info.BaseEnv,
			},
			CronTriggered: true,
		}
		_ = s.stopProcess(mp)
		_, _ = s.launchProcess(mp.Info.Name, req)
	}
	return nil
}

// pauseByName suspends every matching process. A paused process has its
// cron schedule removed (it will not fire) and any running instance is
// stopped; its status becomes StatusPaused so callers can distinguish a
// deliberately-suspended cron task from one merely idle between fires.
func (s *Server) pauseByName(name string) error {
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

// resumeByName reverses pauseByName for every matching process. It
// re-launches through launchProcess, which re-registers the cron
// schedule and returns a cron task to its idle StatusStopped state (or
// a regular process to StatusOnline). Resuming a process that was not
// paused is a no-op that still succeeds, keeping the command idempotent.
func (s *Server) resumeByName(name string) error {
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