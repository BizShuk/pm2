package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/shuk/pm2/cron"
	"github.com/shuk/pm2/process"
)

// Server is the PM2 daemon
type Server struct {
	mu        sync.RWMutex
	processes map[string]*ManagedProcess
	nextID    int
	homeDir   string
	scheduler *cron.Scheduler
}

// ManagedProcess pairs runtime state with the OS process handle
type ManagedProcess struct {
	Info     process.ProcessInfo
	Cmd      *exec.Cmd
	done     chan struct{}
	stopping bool // true when deliberately stopped, suppresses auto-restart
}

func NewServer(homeDir string) *Server {
	return &Server{
		processes: make(map[string]*ManagedProcess),
		homeDir:   homeDir,
		scheduler: cron.New(),
	}
}

// Listen starts the Unix socket server
func (s *Server) Listen(socketPath string) error {
	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	log.Printf("daemon listening on %s", socketPath)
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

		// Identity = name + script path (both must match to allow override)
		s.mu.Lock()
		key := ns + ":" + name
		if existing, ok := s.processes[key]; ok {
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
		logFile = filepath.Join(logDir, name)
	} else {
		logFile = expandHome(logFile)
	}
	errFile := req.ErrorFile
	if errFile == "" {
		errFile = filepath.Join(logDir, name)
	} else {
		errFile = expandHome(errFile)
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

	cmdArgs := append([]string{req.Script}, req.Args...)
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdout = outF
	cmd.Stderr = errF
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Apply environment
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	if err := cmd.Start(); err != nil {
		outF.Close()
		errF.Close()
		return process.ProcessInfo{}, fmt.Errorf("start process: %w", err)
	}

	ns := req.Namespace
	if ns == "" {
		ns = "default"
	}

	s.mu.Lock()
	id := s.nextID
	s.nextID++
	mp := &ManagedProcess{
		Info: process.ProcessInfo{
			ID:          id,
			Namespace:   ns,
			Name:        name,
			PID:         cmd.Process.Pid,
			Status:      process.StatusOnline,
			StartedAt:   time.Now(),
			Script:      req.Script,
			Args:        req.Args,
			Env:         req.Env,
			CronRestart: req.CronRestart,
			LogFile:     logFile,
			ErrorFile:   errFile,
			MaxRestarts: req.MaxRestarts,
		},
		Cmd:  cmd,
		done: make(chan struct{}),
	}
	s.processes[ns+":"+name] = mp
	s.mu.Unlock()

	// Watch for process exit in background
	go s.watchProcess(mp, outF, errF)

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
			log.Printf("cron_restart parse error for %s: %v", name, err)
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
			time.Sleep(1 * time.Second)
			req := &AppStartReq{
				Namespace:   mp.Info.Namespace,
				Name:        mp.Info.Name,
				Script:      mp.Info.Script,
				Args:        mp.Info.Args,
				Env:         mp.Info.Env,
				CronRestart: mp.Info.CronRestart,
				MaxRestarts: mp.Info.MaxRestarts,
				LogFile:     mp.Info.LogFile,
				ErrorFile:   mp.Info.ErrorFile,
				Instances:   1,
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

	s.mu.Lock()
	mp.Info.Status = process.StatusStopped
	mp.Info.PID = 0
	s.mu.Unlock()
	return nil
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
			Namespace:   mp.Info.Namespace,
			Name:        mp.Info.Name,
			Script:      mp.Info.Script,
			Args:        mp.Info.Args,
			Env:         mp.Info.Env,
			CronRestart: mp.Info.CronRestart,
			MaxRestarts: mp.Info.MaxRestarts,
			LogFile:     mp.Info.LogFile,
			ErrorFile:   mp.Info.ErrorFile,
			Instances:   1,
		}
		_ = s.stopProcess(mp)
		_, _ = s.launchProcess(mp.Info.Name, req)
	}
	return nil
}

func (s *Server) deleteByName(name string) error {
	targets := s.findProcesses(name)
	if len(targets) == 0 {
		return fmt.Errorf("process or namespace not found: %s", name)
	}
	for _, mp := range targets {
		_ = s.stopProcess(mp)
		s.mu.Lock()
		delete(s.processes, mp.Info.Namespace+":"+mp.Info.Name)
		s.mu.Unlock()
	}
	return nil
}

func (s *Server) listAll() []process.ProcessInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	infos := make([]process.ProcessInfo, 0, len(s.processes))
	for _, mp := range s.processes {
		infos = append(infos, mp.Info)
	}
	return infos
}

func (s *Server) save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var entries []process.DumpEntry
	for _, mp := range s.processes {
		entries = append(entries, process.DumpEntry{
			Namespace:   mp.Info.Namespace,
			Name:        mp.Info.Name,
			Script:      mp.Info.Script,
			Args:        mp.Info.Args,
			Env:         mp.Info.Env,
			CronRestart: mp.Info.CronRestart,
			Instances:   1,
			MaxRestarts: mp.Info.MaxRestarts,
			LogFile:     mp.Info.LogFile,
			ErrorFile:   mp.Info.ErrorFile,
		})
	}

	dumpPath := filepath.Join(s.homeDir, "dump.json")
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dumpPath, data, 0o644)
}

func (s *Server) resurrect() error {
	dumpPath := filepath.Join(s.homeDir, "dump.json")
	data, err := os.ReadFile(dumpPath)
	if err != nil {
		return fmt.Errorf("no dump found (run pm2 save first): %w", err)
	}
	var entries []process.DumpEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	for _, e := range entries {
		req := &AppStartReq{
			Namespace:   e.Namespace,
			Name:        e.Name,
			Script:      e.Script,
			Args:        e.Args,
			Env:         e.Env,
			CronRestart: e.CronRestart,
			Instances:   e.Instances,
			MaxRestarts: e.MaxRestarts,
			LogFile:     e.LogFile,
			ErrorFile:   e.ErrorFile,
		}
		if _, err := s.startApp(req); err != nil {
			log.Printf("resurrect %s: %v", e.Name, err)
		}
	}
	return nil
}

func (s *Server) findProcesses(target string) []*ManagedProcess {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if target == "all" {
		var list []*ManagedProcess
		for _, mp := range s.processes {
			list = append(list, mp)
		}
		return list
	}

	// 1. ID 匹配
	var idVal int
	if _, err := fmt.Sscan(target, &idVal); err == nil {
		for _, mp := range s.processes {
			if mp.Info.ID == idVal {
				return []*ManagedProcess{mp}
			}
		}
	}

	// 2. Name 匹配
	var matchedByName []*ManagedProcess
	for _, mp := range s.processes {
		if mp.Info.Name == target {
			matchedByName = append(matchedByName, mp)
		}
	}
	if len(matchedByName) > 0 {
		return matchedByName
	}

	// 3. Namespace 匹配
	var matchedByNS []*ManagedProcess
	for _, mp := range s.processes {
		if mp.Info.Namespace == target {
			matchedByNS = append(matchedByNS, mp)
		}
	}
	return matchedByNS
}

func expandHome(path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			if u, err := user.Current(); err == nil {
				home = u.HomeDir
			}
		}
		if home != "" {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
