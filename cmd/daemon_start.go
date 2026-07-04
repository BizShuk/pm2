package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bizshuk/pm2/daemon"
	"github.com/bizshuk/pm2/model"
	"github.com/spf13/cobra"
)

// newDaemonStartCmd returns `pm2 daemon start [--foreground]`.
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
func newDaemonStartCmd() *cobra.Command {
	var foreground bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the PM2 daemon (background by default)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if foreground {
				srv := daemon.NewServer(pm2Home)
				return srv.Listen(socketPath())
			}
			return startDaemonAsBackground()
		},
	}
	cmd.Flags().BoolVarP(&foreground, "foreground", "f", false, "Run the daemon in the foreground (blocking)")
	return cmd
}

// startDaemonAsBackground re-execs the current binary with
// `daemon start --foreground` and detaches it. Stdout/stderr are
// redirected to `~/.pm2/daemon.log` / `~/.pm2/daemon-err.log` so
// the user can `tail -f` them after the fact. Setpgid ensures the
// daemon is its own process group leader (so `pm2 daemon kill` can
// later signal the whole tree if needed).
func startDaemonAsBackground() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	logDir := pm2Home
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

// autoStartDaemon is the silent variant of startDaemonAsBackground
// used by other CLI commands (e.g. `pm2 start`) when they detect no
// daemon is reachable on the socket. Unlike the user-facing
// `pm2 daemon start`, this path does not redirect logs (they'd
// duplicate with the foreground daemon's own redirect), and it
// pings the socket until the daemon is ready to accept RPC — so the
// caller can immediately send a StartApp request without a race.
//
// Kept in this file (not daemon.go) because it is mechanically a
// "spawn the daemon binary" helper, just with different I/O and a
// readiness wait.
func autoStartDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exe, "daemon", "start", "--foreground")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn daemon: %w", err)
	}
	_ = cmd.Process.Release()

	// Wait until daemon is ready (up to 3 seconds)
	sock := socketPath()
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		resp, err := model.SendRequest(sock, model.Request{Command: model.CmdPing})
		if err == nil && resp.OK {
			return nil
		}
	}
	return fmt.Errorf("daemon did not start in time")
}