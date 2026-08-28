// Package runtime owns shared CLI state and daemon RPC infrastructure.
package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/runhistory"
	"github.com/bizshuk/pm2/workflow"
)

const daemonStopMarkerFile = "daemon.stopped"

// pm2Home resolves ~/.config/pm2 on first use, not at package init.
//
// Resolving it in an init() made every pm2 command — including the ones
// that never touch the state directory — die on any process with no
// $HOME, and take the whole binary down before main ran. A LaunchDaemon
// in the system domain is exactly that process: launchd passes it no
// HOME, so `pm2 gpu agent` exited 1 six times in a row without ever
// reaching its own code. Creating the state directory as a side effect
// of starting any command was the same mistake in a quieter form.
//
// A command that genuinely needs the directory still fails loudly, just
// at the point of use, where the error names something the caller asked
// for.
var homeDir = sync.OnceValue(func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot determine home dir:", err)
		os.Exit(1)
	}
	return home
})

var pm2Home = sync.OnceValue(func() string {
	dir := filepath.Join(homeDir(), ".config", "pm2")
	_ = os.MkdirAll(dir, 0o755)
	return dir
})

func socketPath() string {
	return filepath.Join(pm2Home(), "pm2.sock")
}

// PM2Home returns the shared state directory used by CLI command packages.
func PM2Home() string {
	return pm2Home()
}

// SocketPath returns the daemon socket used by CLI command packages.
func SocketPath() string {
	return socketPath()
}

// TaskLogsDir returns the single directory every managed task logs into,
// ~/.config/pm2/tasks/logs. It is not created here — the log browser reads
// it, and a reader must not invent the directory it is reporting on.
func TaskLogsDir() string {
	return process.TaskLogsDir(pm2Home())
}

// DaemonLogsDir returns ~/.config/pm2/logs, where the daemon's own rotating
// log and the raw stderr its supervisor redirects both live. It is kept
// apart from the managed-task logs because the two have different owners:
// one is written by pm2, the others by the programs pm2 supervises.
func DaemonLogsDir() string {
	return filepath.Join(pm2Home(), "logs")
}

// WorkflowsDir returns ~/.config/pm2/workflows, holding the registered
// workflow definitions and every run's per-stage logs.
func WorkflowsDir() string {
	return workflow.Dir(pm2Home())
}

// RunHistoryStore opens the run journals for reading.
//
// `pm2 workflow runs` and `pm2 workflow show` read them directly rather
// than asking the daemon, for the same reason `pm2 logs monitor` reads
// the filesystem: making something happen needs the daemon, but what
// already happened is a file. History therefore survives the daemon
// being down and outlives the workflow that produced it.
func RunHistoryStore() *runhistory.Store {
	return runhistory.NewStore(pm2Home())
}

// DaemonStopMarkerPath returns the durable marker that disables silent
// daemon auto-spawn after an explicit `pm2 daemon stop`.
func DaemonStopMarkerPath() string {
	return filepath.Join(pm2Home(), daemonStopMarkerFile)
}
