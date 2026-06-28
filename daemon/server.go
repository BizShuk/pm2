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
	"sync"
	"syscall"
	"time"

	_ "github.com/bizshuk/gosdk/log" // initialize gosdk slog default handler

	"github.com/bizshuk/pm2/cron"
	"github.com/bizshuk/pm2/process"
	"github.com/fsnotify/fsnotify"
	"github.com/mitchellh/go-homedir"
	"os/user"
)

// Server is the PM2 daemon
type Server struct {
	mu           sync.RWMutex
	processes    map[string]*ManagedProcess
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
	Watcher  *fsnotify.Watcher
}

func NewServer(homeDir string) *Server {
	return &Server{
		processes:    make(map[string]*ManagedProcess),
		homeDir:      homeDir,
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
	var req Request
	if err := ReadJSON(conn, &req); err != nil {
		_ = WriteJSON(conn, Response{Error: err.Error()})
		return
	}

	var resp Response
	switch req.Command {
	case CmdPing:
		resp = Response{OK: true}

	case CmdStart:
		if req.App == nil {
			resp = Response{Error: "missing app config"}
		} else {
			infos, err := s.startApp(req.App)
			if err != nil {
				resp = Response{Error: err.Error()}
			} else {
				payload, _ := json.Marshal(infos)
				resp = Response{OK: true, Payload: payload}
			}
		}

	case CmdStop:
		err := s.stopByName(req.Name)
		if err != nil {
			resp = Response{Error: err.Error()}
		} else {
			resp = Response{OK: true}
		}

	case CmdRestart:
		err := s.restartByName(req.Name)
		if err != nil {
			resp = Response{Error: err.Error()}
		} else {
			resp = Response{OK: true}
		}

	case CmdDelete:
		err := s.deleteByName(req.Name)
		if err != nil {
			resp = Response{Error: err.Error()}
		} else {
			resp = Response{OK: true}
		}

	case CmdList:
		infos := s.listAll()
		payload, _ := json.Marshal(infos)
		resp = Response{OK: true, Payload: payload}

	case CmdSave:
		if err := s.save(); err != nil {
			resp = Response{Error: err.Error()}
		} else {
			resp = Response{OK: true}
		}

	case CmdResurrect:
		if err := s.resurrect(); err != nil {
			resp = Response{Error: err.Error()}
		} else {
			resp = Response{OK: true}
		}

	case CmdKill:
		// Gracefully stop every managed process, reply, then exit the daemon.
		s.killAll()
		resp = Response{OK: true}
		// Exit after the response is flushed below; the small delay lets
		// WriteJSON at the end of handleConn complete first.
		go func() {
			time.Sleep(150 * time.Millisecond)
			slog.Info("daemon shutting down via kill command")
			os.Exit(0)
		}()

	default:
		resp = Response{Error: fmt.Sprintf("unknown command: %s", req.Command)}
	}

	_ = WriteJSON(conn, resp)
}

func (s *Server) startApp(req *AppStartReq) ([]process.ProcessInfo, error) {
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

		s.mu.Lock()
		key := ns + ":" + name
		var existing *ManagedProcess
		var ok bool
		if existing, ok = s.processes[key]; ok {
			if existing.Info.Script != req.Script {
				s.mu.Unlock()
				return infos, fmt.Errorf(
					"process %q already exists with script %q; use 'pm2 delete %s' first or use a different name",
					name, existing.Info.Script, name,
				)
			}
			s.mu.Unlock()
			_ = s.stopProcess(existing)
		} else {
			if req.ConfigFile != "" {
				for _, mp := range s.processes {
					if mp.Info.Name == name && mp.Info.ConfigFile == req.ConfigFile {
						existing = mp
						break
					}
				}
				if existing != nil {
					if existing.Info.Script != req.Script {
						s.mu.Unlock()
						return infos, fmt.Errorf(
							"process %q already exists with script %q; use 'pm2 delete %s' first or use a different name",
							name, existing.Info.Script, name,
						)
					}
					s.mu.Unlock()
					_ = s.stopProcess(existing)
				} else {
					s.mu.Unlock()
				}
			} else {
				s.mu.Unlock()
			}
		}

		info, err := s.launchProcess(name, req)
		if err != nil {
			return infos, fmt.Errorf("launch %s: %w", name, err)
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func (s *Server) launchProcess(name string, req *AppStartReq) (process.ProcessInfo, error) {
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

	s.mu.Lock()
	var id int
	var lastCronAt time.Time
	var lastCronStatus string
	var oldKey string
	existing, ok := s.processes[ns+":"+name]
	if !ok && req.ConfigFile != "" {
		for k, mp := range s.processes {
			if mp.Info.Name == name && mp.Info.ConfigFile == req.ConfigFile {
				existing = mp
				ok = true
				oldKey = k
				break
			}
		}
	}

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
			ID:             id,
			Namespace:      ns,
			Name:           name,
			PID:            pid,
			Status:         status,
			StartedAt:      startedAt,
			Script:         req.Script,
			Args:           req.Args,
			Env:            req.Env,
			CronRestart:    req.CronRestart,
			Cron:           req.Cron,
			LastCronAt:     lastCronAt,
			LastCronStatus: lastCronStatus,
			LogFile:        logFile,
			ErrorFile:      errFile,
			MaxRestarts:    req.MaxRestarts,
			ConfigDir:      req.ConfigDir,
			Version:        version,
			User:           currentUser,
			Watch:          req.Watch,
			ConfigFile:     req.ConfigFile,
			Restarts:       restarts,
			CWD:            req.CWD,
			BaseEnv:        req.BaseEnv,
		},
		Cmd:     cmd,
		done:    make(chan struct{}),
		Watcher: watcher,
	}
	s.processes[ns+":"+name] = mp
	if oldKey != "" && oldKey != ns+":"+name {
		delete(s.processes, oldKey)
	}
	s.mu.Unlock()

	if !isCronTask {
		// Watch for process exit in background
		go s.watchProcess(mp, outF, errF)
	}

	// Register cron restart if configured
	if req.CronRestart != "" {
		if err := s.scheduler.Register(name, req.CronRestart, func() {
			firedAt := time.Now()
			restartErr := s.restartByName(name)
			// Write last-run info onto the newly launched process (map was replaced by restartByName)
			s.mu.Lock()
			key := ns + ":" + name
			if p, ok := s.processes[key]; ok {
				p.Info.LastCronAt = firedAt
				if restartErr != nil {
					p.Info.LastCronStatus = "failed"
				} else {
					p.Info.LastCronStatus = "ok"
				}
			}
			s.mu.Unlock()
		}); err != nil {
			slog.Info("cron_restart parse error", "name", name, "err", err)
		}
	}

	// Register cron schedule if configured
	if req.Cron != "" {
		if err := s.scheduler.Register(name, req.Cron, func() {
			s.triggerCron(ns, name, req)
		}); err != nil {
			slog.Info("cron parse error", "name", name, "err", err)
		}
	}

	return mp.Info, nil
}

func (s *Server) watchProcess(mp *ManagedProcess, outF, errF *os.File) {
	err := mp.Cmd.Wait()
	outF.Close()
	errF.Close()
	close(mp.done)

	s.mu.Lock()
	defer s.mu.Unlock()

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
		go func() {
			time.Sleep(s.RestartDelay)
			s.mu.Lock()
			// Check if the process still exists in our map and is the same instance, and is not stopping
			key := mp.Info.Namespace + ":" + mp.Info.Name
			current, exists := s.processes[key]
			if !exists || current != mp || mp.stopping {
				s.mu.Unlock()
				return
			}
			s.mu.Unlock()

			req := &AppStartReq{
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
				Instances:   1,
				ConfigFile:  mp.Info.ConfigFile,
				CWD:         mp.Info.CWD,
				BaseEnv:     mp.Info.BaseEnv,
			}
			_, _ = s.launchProcess(mp.Info.Name, req)
		}()
	}
}

func (s *Server) stopProcess(mp *ManagedProcess) error {
	s.mu.Lock()
	mp.stopping = true
	mp.Info.Status = process.StatusStopping
	s.mu.Unlock()

	// Cancel cron before stopping
	s.scheduler.Remove(mp.Info.Name)

	if mp.Watcher != nil {
		_ = mp.Watcher.Close()
		mp.Watcher = nil
	}

	if mp.Cmd != nil {
		if mp.Cmd.Process != nil {
			if err := mp.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
				_ = mp.Cmd.Process.Kill()
			}
		}

		// Wait with timeout
		select {
		case <-mp.done:
		case <-time.After(5 * time.Second):
			if mp.Cmd.Process != nil {
				_ = mp.Cmd.Process.Kill()
			}
		}
	}

	s.mu.Lock()
	mp.Info.Status = process.StatusStopped
	mp.Info.PID = 0
	s.mu.Unlock()
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
		req := &AppStartReq{
			Namespace:     mp.Info.Namespace,
			Name:          mp.Info.Name,
			Script:        mp.Info.Script,
			Args:          mp.Info.Args,
			Env:           mp.Info.Env,
			CronRestart:   mp.Info.CronRestart,
			Cron:          mp.Info.Cron,
			CronTriggered: true,
			Watch:         mp.Info.Watch,
			MaxRestarts:   mp.Info.MaxRestarts,
			LogFile:       mp.Info.LogFile,
			ErrorFile:     mp.Info.ErrorFile,
			Instances:     1,
			Version:       mp.Info.Version,
			ConfigFile:    mp.Info.ConfigFile,
			CWD:           mp.Info.CWD,
			BaseEnv:       mp.Info.BaseEnv,
		}
		_ = s.stopProcess(mp)
		_, _ = s.launchProcess(mp.Info.Name, req)
	}
	return nil
}

func (s *Server) triggerCron(ns, name string, originalReq *AppStartReq) {
	s.mu.Lock()
	key := ns + ":" + name
	mp, ok := s.processes[key]
	s.mu.Unlock()
	if !ok {
		return
	}

	triggerReq := *originalReq
	triggerReq.CronTriggered = true

	firedAt := time.Now()

	_ = s.stopProcess(mp)
	_, err := s.launchProcess(name, &triggerReq)

	s.mu.Lock()
	if p, ok := s.processes[key]; ok {
		p.Info.LastCronAt = firedAt
		if err != nil {
			p.Info.LastCronStatus = "failed"
		} else {
			p.Info.LastCronStatus = "ok"
		}
	}
	s.mu.Unlock()
}
