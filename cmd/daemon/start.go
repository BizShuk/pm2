package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	daemonruntime "github.com/bizshuk/pm2/daemon"
	"github.com/spf13/cobra"
)

// StartCmd is `pm2 daemon start [--foreground]`.
//
// In background mode (default) the CLI spawns itself with the
// `daemon start --foreground` argv so the foreground path is the
// single source of truth for what "running the daemon" actually
// means — both user-facing `pm2 daemon start` and internal
// auto-start paths call into the same exec.
//
// In `--foreground` mode we call `daemon.NewServer(...).Listen(...)`
// directly so Ctrl+C / SIGTERM cleanly tears the daemon down. This
// is also the path the launchd / systemd units use.
var StartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the PM2 daemon (alias: pm2 start)",
	Args:  cobra.NoArgs,
	RunE:  RunStart,
}

func init() {
	BindStartFlags(StartCmd)
}

// BindStartFlags attaches the daemon-start flags shared by `pm2 daemon start`
// and the root `pm2 start` alias.
func BindStartFlags(command *cobra.Command) {
	command.Flags().BoolP("foreground", "f", false, "Run the daemon in the foreground (blocking)")
}

// RunStart is the shared handler for `pm2 daemon start` and `pm2 start`.
func RunStart(cmd *cobra.Command, _ []string) error {
	foreground, err := cmd.Flags().GetBool("foreground")
	if err != nil {
		return fmt.Errorf("read --foreground: %w", err)
	}
	if foreground {
		srv := daemonruntime.NewServer(cliruntime.PM2Home())
		return srv.Listen(cliruntime.SocketPath())
	}
	return startAsBackground()
}

// startAsBackground re-execs the current binary with
// `daemon start --foreground` and detaches it. Stdout/stderr are
// redirected to `~/.pm2/daemon.log` / `~/.pm2/daemon-err.log` so
// the user can `tail -f` them after the fact. Setpgid ensures the
// daemon is its own process group leader (so `pm2 daemon kill` can
// later signal the whole tree if needed).
func startAsBackground() error {
	// Clear the stop marker so future CLI invocations can auto-respawn
	// again. The user just explicitly asked for a daemon — the
	// auto-spawn opt-out from a previous `pm2 daemon stop` is no
	// longer in effect. Tolerate a missing marker (start may be the
	// first daemon command run on a fresh install).
	if err := removeStopMarker(); err != nil {
		// Best-effort: a marker we can't remove is a permission
		// problem the user needs to know about, since otherwise
		// their `daemon start` would race with auto-spawn refusal.
		fmt.Fprintf(os.Stderr, "warning: could not remove stop marker: %v\n", err)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	logDir := cliruntime.PM2Home()
	_ = os.MkdirAll(logDir, 0o755)
	logFile := filepath.Join(logDir, "daemon.log")
	errFile := filepath.Join(logDir, "daemon-err.log")

	outF, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer outF.Close()

	errF, err := os.OpenFile(errFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer errF.Close()

	cmd := exec.Command(exe, "daemon", "start", "--foreground")
	cmd.Stdout = outF
	cmd.Stderr = errF
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn daemon background: %w", err)
	}
	_ = cmd.Process.Release()

	fmt.Println("PM2 daemon started in the background.")
	return nil
}
