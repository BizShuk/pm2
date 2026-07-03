package executor

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bizshuk/pm2/model"
	"github.com/fsnotify/fsnotify"
	"github.com/mitchellh/go-homedir"
)

// stopTimeout is how long Stop waits for the process group to exit
// after SIGTERM before escalating to SIGKILL.
const stopTimeout = 5 * time.Second

// Executor encapsulates the OS-level operations for a single managed
// process: launch (fork+exec), watch (cmd.Wait), stop (signal+kill),
// file-watching (fsnotify).
//
// It does NOT hold any ProcessRegistry lock; state updates flow back
// to the caller via callbacks.
//
// Lock direction (Phase 4 invariant):
//   - The caller (Server) may invoke Executor methods while holding
//     the registry lock, because Executor holds NO lock during its
//     execution.
//   - Executor NEVER calls back into the registry — it only signals
//     via callbacks (onFileChanged / onExit / onStopping / onStopped).
//     The caller's callback implementations take the registry lock
//     internally (via ProcessRegistry.UpdateInfo) and never hold it
//     across any blocking call.
type Executor struct {
	homeDir string
}

// NewExecutor returns a ready-to-use Executor rooted at homeDir
// (the daemon state directory used to resolve default log paths).
func NewExecutor(homeDir string) *Executor {
	return &Executor{homeDir: homeDir}
}

// HomeDir returns the daemon state directory passed to NewExecutor.
// Tests use this to build paths under t.TempDir().
func (e *Executor) HomeDir() string { return e.homeDir }

// StartResult is the OS-side artifact bundle returned by Start. The
// caller stores these on its own ManagedProcess (or equivalent) and
// is responsible for closing the file handles / watcher at the right
// time (Start has already closed them on the failure paths).
//
// Fields:
//   - Cmd:     the started *exec.Cmd (Process.Pid populated after Start)
//              nil if req.Cron != "" and !req.CronTriggered (cron task)
//   - OutF:    opened *os.File for stdout (caller passes to Watch)
//              nil if cron task
//   - ErrF:    opened *os.File for stderr (caller passes to Watch)
//              nil if cron task
//   - Watcher: fsnotify.Watcher for the script file (caller passes to Watch)
//              nil if req.Watch == false OR cron task
//   - LogFile: resolved absolute path (always set)
//   - ErrFile: resolved absolute path (always set)
type StartResult struct {
	Cmd     *exec.Cmd
	OutF    *os.File
	ErrF    *os.File
	Watcher *fsnotify.Watcher
	LogFile string
	ErrFile string
}

// Start resolves log paths, opens log files, builds the *exec.Cmd via
// BuildCommand, and starts the process. If req.Watch is true, also
// creates an fsnotify watcher whose debounced trigger fires onFileChanged.
//
// onFileChanged is the caller-supplied restart hook (the Server passes
// a closure that calls its restartByName). It may be nil if the caller
// does not want a hook attached — the watcher will still be created
// and events will still be logged, but no callback fires.
//
// Start does NOT add the process to any registry. The caller is
// responsible for building its ManagedProcess and registering it.
// Start does NOT spawn the Watch goroutine — the caller does that.
func (e *Executor) Start(req *model.AppStartReq, name string, onFileChanged func()) (*StartResult, error) {
	logDir := filepath.Join(e.homeDir, "logs")
	_ = os.MkdirAll(logDir, 0o755)

	logFile := resolveLogPath(req.LogFile, req.OutFile, req.ConfigDir, logDir, name)
	errFile := resolveLogPath(req.ErrorFile, "", req.ConfigDir, logDir, name)

	// Create directories for log files (idempotent).
	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(errFile), 0o755); err != nil {
		return nil, fmt.Errorf("create error log directory: %w", err)
	}

	// Ensure log files exist (os.O_CREATE alone would race with append).
	ensureLogFile(logFile)
	ensureLogFile(errFile)

	outF, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	errF, err := os.OpenFile(errFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		outF.Close()
		return nil, err
	}

	isCronTask := req.Cron != "" && !req.CronTriggered
	isPaused := req.Paused
	if isCronTask || isPaused {
		// Cron task (idle between fires) and Paused entry (cron schedule
		// deliberately suspended — see regression test
		// TestPausedCronTaskSurvivesResurrect) both share the same
		// launch shape: no child to fork, no watch to wire up, but the
		// log file paths must still be present on mp.Info so `pm2 logs`
		// resolves them. Close outF/errF so they do not stay held
		// open while idle.
		outF.Close()
		errF.Close()
		return &StartResult{LogFile: logFile, ErrFile: errFile}, nil
	}

	// Resolve the working directory: req.CWD wins; otherwise fall back
	// to the directory of the originating ecosystem.config.js.
	workDir := req.CWD
	if workDir == "" {
		workDir = filepath.Dir(req.ConfigFile)
	}
	if workDir != "" {
		if _, err := os.Stat(workDir); err != nil {
			// Fall back to the daemon's own cwd so os.StartProcess
			// doesn't fail with ENOENT (e.g. tests pass a fake
			// /path/to/ecosystem.config.js).
			workDir = ""
		}
	}

	// Prefer the CLI's environment snapshot; fall back to daemon's.
	base := req.BaseEnv
	if len(base) == 0 {
		base = os.Environ()
	}

	cmd := BuildCommand(req.Script, req.Args, base, req.Env, workDir)
	cmd.Stdout = outF
	cmd.Stderr = errF

	if err := cmd.Start(); err != nil {
		outF.Close()
		errF.Close()
		return nil, fmt.Errorf("start process: %w", err)
	}

	var watcher *fsnotify.Watcher
	if req.Watch {
		// NewFileWatcher returns (nil, nil) on fsnotify setup error;
		// log a warning but do NOT fail the launch — file watching is
		// a best-effort convenience, not a critical path.
		w, werr := NewFileWatcher(req.Script, onFileChanged)
		if werr != nil {
			slog.Info("file watcher create failed", "name", name, "err", werr)
		}
		watcher = w
	}

	return &StartResult{
		Cmd:     cmd,
		OutF:    outF,
		ErrF:    errF,
		Watcher: watcher,
		LogFile: logFile,
		ErrFile: errFile,
	}, nil
}

// Watch blocks on cmd.Wait(), then:
//  1. Closes the log files (outF, errF) — required so the files do
//     not stay held open after the process exits.
//  2. Closes the watcher if non-nil.
//  3. Closes the done channel so Stop can unblock.
//  4. Calls onExit(err) so the caller can update state and decide
//     whether to auto-restart.
//
// Watch holds NO locks. The onExit callback may take whatever locks
// the caller wants; Watch does not care.
//
// Any of (cmd, outF, errF, watcher, done) may be nil (e.g. cron task
// where cmd is nil, or when no watch is configured) — Watch tolerates
// them.
func (e *Executor) Watch(
	cmd *exec.Cmd,
	outF, errF *os.File,
	watcher *fsnotify.Watcher,
	done chan struct{},
	onExit func(err error),
) {
	var waitErr error
	if cmd != nil {
		waitErr = cmd.Wait()
	}
	if outF != nil {
		outF.Close()
	}
	if errF != nil {
		errF.Close()
	}
	if watcher != nil {
		_ = watcher.Close()
	}
	if done != nil {
		close(done)
	}
	if onExit != nil {
		onExit(waitErr)
	}
}

// Stop sends SIGTERM to the *whole process group* (negative pid =
// process group ID in kill(2)) so children of the bash leader are
// also killed. Waits up to stopTimeout for done, then escalates to SIGKILL.
//
// onStopping is called before any signal is sent (so the caller can
// set stopping=true + Status=StatusStopping). onStopped is called
// after the process is fully gone (so the caller can set
// Status=StatusStopped + PID=0). If cmd is nil or cmd.Process is nil
// (e.g. cron task), Stop returns after invoking both callbacks.
//
// Stop holds NO registry lock. The onStopping/onStopped callbacks
// may take whatever locks the caller wants.
func (e *Executor) Stop(
	cmd *exec.Cmd,
	done <-chan struct{},
	onStopping func(),
	onStopped func(),
) error {
	if onStopping != nil {
		onStopping()
	}

	if cmd != nil && cmd.Process != nil {
		pid := cmd.Process.Pid
		// Process-group kill (see BuildCommand: Setpgid: true on every
		// spawn). ESRCH = group already gone.
		if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
			if !errors.Is(err, syscall.ESRCH) {
				_ = cmd.Process.Kill()
			}
		}

		// Wait with timeout, then escalate to the process group.
		if done != nil {
			select {
			case <-done:
			case <-time.After(stopTimeout):
				if cmd != nil && cmd.Process != nil {
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
			}
		}
	}

	if onStopped != nil {
		onStopped()
	}
	return nil
}

// resolveLogPath picks the actual on-disk path for a log file,
// expanding ~ and applying the documented precedence:
//  1. logFile (explicit request)
//  2. altFile (req.OutFile fallback for the stdout file)
//  3. configDir/logs/daemon.log (if ConfigDir set and no logFile)
//  4. <home>/logs/<name> (default)
//
// It also expands ~ via homedir.Expand on the explicit logFile path.
func resolveLogPath(logFile, altFile, configDir, logDir, name string) string {
	resolved := logFile
	if resolved == "" {
		resolved = altFile
	}
	if resolved == "" && configDir != "" {
		resolved = filepath.Join(configDir, "logs", "daemon.log")
	}
	if resolved == "" {
		resolved = filepath.Join(logDir, name)
	} else {
		if h, err := homedir.Expand(resolved); err == nil {
			resolved = h
		}
	}
	return resolved
}

// ensureLogFile creates the file (empty) if it does not exist.
// We need it before os.OpenFile(APPEND) because the file must exist
// before appending on platforms that disallow creating+appending in
// one call.
func ensureLogFile(path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
		}
	}
}