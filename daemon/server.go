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

	"github.com/fsnotify/fsnotify"
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
	Watcher  *fsnotify.Watcher
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
	s.StartMetricsCollector()
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
		logFile = expandHome(logFile)
	}
	errFile := req.ErrorFile
	if errFile == "" && req.ConfigDir != "" {
		errFile = filepath.Join(req.ConfigDir, "logs", "daemon.err")
	}
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
		cmdArgs := append([]string{req.Script}, req.Args...)
		cmd = exec.Command(cmdArgs[0], cmdArgs[1:]...)
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
		pid = cmd.Process.Pid
		startedAt = time.Now()
	}

	ns := req.Namespace
	if ns == "" {
		ns = "default"
	}

	var watcher *fsnotify.Watcher
	if req.Watch {
		var err error
		watcher, err = fsnotify.NewWatcher()
		if err == nil {
			err = watcher.Add(req.Script)
			if err != nil {
				_ = watcher.Close()
				watcher = nil
				log.Printf("watch error for %s: %v", name, err)
			} else {
				go func(pName string, w *fsnotify.Watcher) {
					var timer *time.Timer
					const debounceDuration = 500 * time.Millisecond
					for {
						select {
						case event, ok := <-w.Events:
							if !ok {
								return
							}
							if event.Has(fsnotify.Write) || event.Has(fsnotify.Rename) {
								if timer != nil {
									timer.Stop()
								}
								timer = time.AfterFunc(debounceDuration, func() {
									log.Printf("File changed: %s, restarting %s", event.Name, pName)
									_ = s.restartByName(pName)
								})
							}
						case err, ok := <-w.Errors:
							if !ok {
								return
							}
							log.Printf("watcher error for %s: %v", pName, err)
						}
					}
				}(name, watcher)
			}
		} else {
			log.Printf("create watcher error for %s: %v", name, err)
		}
	}

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

	if ok {
		id = existing.Info.ID
		lastCronAt = existing.Info.LastCronAt
		lastCronStatus = existing.Info.LastCronStatus
	} else {
		id = s.nextID
		s.nextID++
	}

	if (req.Cron != "" || req.CronRestart != "") && !isCronTask {
		lastCronAt = startedAt
		lastCronStatus = "ok"
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
			User:           getCurrentUser(),
			Watch:          req.Watch,
			ConfigFile:     req.ConfigFile,
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
			log.Printf("cron_restart parse error for %s: %v", name, err)
		}
	}

	// Register cron schedule if configured
	if req.Cron != "" {
		if err := s.scheduler.Register(name, req.Cron, func() {
			s.triggerCron(ns, name, req)
		}); err != nil {
			log.Printf("cron parse error for %s: %v", name, err)
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
				Watch:       mp.Info.Watch,
				MaxRestarts: mp.Info.MaxRestarts,
				LogFile:     mp.Info.LogFile,
				ErrorFile:   mp.Info.ErrorFile,
				Instances:   1,
				ConfigFile:  mp.Info.ConfigFile,
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
			Cron:        mp.Info.Cron,
			Instances:   1,
			MaxRestarts: mp.Info.MaxRestarts,
			LogFile:     mp.Info.LogFile,
			OutFile:     mp.Info.LogFile,
			ErrorFile:   mp.Info.ErrorFile,
			ConfigDir:   mp.Info.ConfigDir,
			Watch:       mp.Info.Watch,
			Version:     mp.Info.Version,
			ConfigFile:  mp.Info.ConfigFile,
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
			Cron:        e.Cron,
			Watch:       e.Watch,
			Instances:   e.Instances,
			MaxRestarts: e.MaxRestarts,
			LogFile:     e.LogFile,
			OutFile:     e.OutFile,
			ErrorFile:   e.ErrorFile,
			ConfigDir:   e.ConfigDir,
			Version:     e.Version,
			ConfigFile:  e.ConfigFile,
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

func getCurrentUser() string {
	u, err := user.Current()
	if err != nil {
		return "unknown"
	}
	return u.Username
}

func getAppVersion(scriptPath string) string {
	dir := filepath.Dir(scriptPath)
	for i := 0; i < 5; i++ {
		pkgPath := filepath.Join(dir, "package.json")
		if _, err := os.Stat(pkgPath); err == nil {
			data, err := os.ReadFile(pkgPath)
			if err == nil {
				var pkg struct {
					Version string `json:"version"`
				}
				if err := json.Unmarshal(data, &pkg); err == nil && pkg.Version != "" {
					return pkg.Version
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "-"
}

func getProcessMetrics(pid int) (float64, uint64) {
	if pid <= 0 {
		return 0, 0
	}
	out, err := exec.Command("ps", "-p", fmt.Sprintf("%d", pid), "-o", "%cpu,rss").Output()
	if err != nil {
		return 0, 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, 0
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 2 {
		return 0, 0
	}
	var cpu float64
	var rss uint64
	_, _ = fmt.Sscanf(fields[0], "%f", &cpu)
	_, _ = fmt.Sscanf(fields[1], "%d", &rss)
	return cpu, rss * 1024
}

func (s *Server) StartMetricsCollector() {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			s.mu.Lock()
			for _, mp := range s.processes {
				if mp.Info.PID > 0 && mp.Info.Status == process.StatusOnline {
					cpu, mem := getProcessMetrics(mp.Info.PID)
					mp.Info.CPU = cpu
					mp.Info.Memory = mem
				} else {
					mp.Info.CPU = 0
					mp.Info.Memory = 0
				}
			}
			s.mu.Unlock()
		}
	}()
}

