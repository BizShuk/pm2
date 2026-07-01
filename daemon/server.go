package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "github.com/bizshuk/gosdk/log" // initialize gosdk slog default handler

	"github.com/bizshuk/pm2/cron"
	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
	"github.com/fsnotify/fsnotify"
	"github.com/mitchellh/go-homedir"
	"os/user"
)

// Server is the PM2 daemon
type Server struct {
	reg          *ProcessRegistry
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
		scheduler:    cron.New(),
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

func (s *Server) launchProcess(name string, req *model.AppStartReq) (process.ProcessInfo, error) {
	logDir := filepath.Join(s.homeDir, "logs")
	_ = os.MkdirAll(logDir, 0o755)

	logFile := req.LogFile
	if logFile == "" {
		logFile = req.OutFile
	}
	if logFile == "" && req.ConfigDir != "" {
		logFile = filepath.Join(req.ConfigDir, "logs", "daemon.log")
	}
	if logFile == "" {
		logFile = filepath.Join(logDir, name)
	} else {
		if h, err := homedir.Expand(logFile); err == nil {
			logFile = h
		}
	}
	errFile := req.ErrorFile
	if errFile == "" && req.ConfigDir != "" {
		errFile = filepath.Join(req.ConfigDir, "logs", "daemon.err")
	}
	if errFile == "" {
		errFile = filepath.Join(logDir, name)
	} else {
		if h, err := homedir.Expand(errFile); err == nil {
			errFile = h
		}
	}

	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		return process.ProcessInfo{}, fmt.Errorf("create log directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(errFile), 0o755); err != nil {
		return process.ProcessInfo{}, fmt.Errorf("create error log directory: %w", err)
	}

	// Ensure log files exist
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return process.ProcessInfo{}, fmt.Errorf("create log file: %w", err)
		}
		f.Close()
	}
	if _, err := os.Stat(errFile); os.IsNotExist(err) {
		f, err := os.OpenFile(errFile, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return process.ProcessInfo{}, fmt.Errorf("create error log file: %w", err)
		}
		f.Close()
	}

	outF, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return process.ProcessInfo{}, err
	}
	errF, err := os.OpenFile(errFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		outF.Close()
		return process.ProcessInfo{}, err
	}

	var cmd *exec.Cmd
	var startedAt time.Time
	var pid int
	status := process.StatusOnline

	isCronTask := req.Cron != "" && !req.CronTriggered

	if isCronTask {
		status = process.StatusStopped
		outF.Close()
		errF.Close()
	} else {
		// Resolve the working directory: req.CWD wins; otherwise fall back to
		// the directory of the originating ecosystem.config.js.
		workDir := req.CWD
		if workDir == "" {
			workDir = filepath.Dir(req.ConfigFile)
		}
		// Fall back to the daemon's own cwd if the resolved workDir does not
		// exist on disk (e.g. tests pass a fake /path/to/ecosystem.config.js
		// and we don't want os.StartProcess to fail with ENOENT).
		if workDir != "" {
			if _, err := os.Stat(workDir); err != nil {
				workDir = ""
			}
		}

		// Prefer the CLI's environment snapshot; fall back to daemon's environ.
		base := req.BaseEnv
		if len(base) == 0 {
			base = os.Environ()
		}

		cmd = buildCommand(req.Script, req.Args, base, req.Env, workDir)
		cmd.Stdout = outF
		cmd.Stderr = errF

		if err := cmd.Start(); err != nil {
			outF.Close()
			errF.Close()
			return process.ProcessInfo{}, fmt.Errorf("start process: %w", err)
		}
		pid = cmd.Process.Pid
		startedAt = time.Now()
	}

	ns := req.Namespace
	if ns == "" {
		ns = "default"
	}

	watcher, _ := s.startFileWatcher(req, name)

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
				// see launchProcess's homedir.Expand block above.
				LogFile:   logFile,
				ErrorFile: errFile,
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
		Cmd:     cmd,
		done:    make(chan struct{}),
		Watcher: watcher,
	}
	s.reg.processes[ns+":"+name] = mp
	if oldKey != "" && oldKey != ns+":"+name {
		delete(s.reg.processes, oldKey)
	}

	if !isCronTask {
		// Watch for process exit in background
		go s.watchProcess(mp, outF, errF)
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
	// is race-free against watchProcess's UpdateInfo writes to mp.Info
	// fields (PID / Status / Restarts).
	info := mp.Info
	return info, nil
}

func (s *Server) watchProcess(mp *ManagedProcess, outF, errF *os.File) {
	err := mp.Cmd.Wait()
	outF.Close()
	errF.Close()
	close(mp.done)

	key := mp.Info.Namespace + ":" + mp.Info.Name
	shouldRestart := false
	s.reg.UpdateInfo(key, func(mp *ManagedProcess) {
		mp.Info.PID = 0
		if err != nil {
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

func (s *Server) stopProcess(mp *ManagedProcess) error {
	key := mp.Info.Namespace + ":" + mp.Info.Name
	s.reg.UpdateInfo(key, func(mp *ManagedProcess) {
		mp.stopping = true
		mp.Info.Status = process.StatusStopping
	})

	// Cancel cron before stopping. The scheduler keys entries by
	// "namespace:name" (see launchProcess), so the Remove key must
	// match — otherwise the cron entry leaks and fires on a dead mp.
	s.scheduler.Remove(key)

	if mp.Watcher != nil {
		_ = mp.Watcher.Close()
		mp.Watcher = nil
	}

	if mp.Cmd != nil {
		if mp.Cmd.Process != nil {
			pid := mp.Cmd.Process.Pid
			// Send SIGTERM to the *whole process group* (negative pid
			// = process group ID in kill(2)). The daemon sets Setpgid
			// on every spawned process (see builder.go), so the bash
			// leader + every descendant share this pgrp. Sending only
			// to the bash leader would leave the child processes
			// orphaned — re-parented to PID 1, still holding ports /
			// files / FDs, and invisible to pm2 list. The fix matches
			// what `pm2 start "sh -c 'node server.js &'"` has always
			// needed but never done.
			if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
				// ESRCH = group already gone (process died before we
				// got here). Anything else (EPERM, etc.) — fall back
				// to the single-process kill which bypasses signal
				// handlers.
				if err != syscall.ESRCH {
					_ = mp.Cmd.Process.Kill()
				}
			}
		}

		// Wait with timeout, then escalate to the process group.
		select {
		case <-mp.done:
		case <-time.After(5 * time.Second):
			if mp.Cmd != nil && mp.Cmd.Process != nil {
				_ = syscall.Kill(-mp.Cmd.Process.Pid, syscall.SIGKILL)
			}
		}
	}

	s.reg.UpdateInfo(key, func(mp *ManagedProcess) {
		mp.Info.Status = process.StatusStopped
		mp.Info.PID = 0
	})
	return nil
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
