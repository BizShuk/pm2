package daemon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	daemonruntime "github.com/bizshuk/pm2/daemon"
	"github.com/bizshuk/pm2/daemon/web"
	"github.com/bizshuk/pm2/model"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
	command.Flags().String("web-host", "", "Address the web dashboard binds to (default "+web.DefaultHost+")")
	command.Flags().Int("web-port", -1, "Port for the web dashboard and webhook; 0 disables it (default "+strconv.Itoa(web.DefaultPort)+")")
}

// WebConfig resolves the dashboard's address, most specific source first:
// the flag, then a flat viper key, then the package default.
//
// The viper key must stay flat. gosdk configures AutomaticEnv with the
// APP prefix, and a nested key is silently ignored by it — writing
// web.port would read nothing at all and leave the operator wondering
// why their setting had no effect. Hence web_host / web_port, i.e.
// APP_WEB_HOST / APP_WEB_PORT.
//
// This is also the only place in the tree that reads these keys: the
// daemon package takes a string and an int and never touches viper.
func WebConfig(cmd *cobra.Command) (string, int) {
	host := viper.GetString("web_host")
	if host == "" {
		host = web.DefaultHost
	}
	if flagHost, err := cmd.Flags().GetString("web-host"); err == nil && flagHost != "" {
		host = flagHost
	}

	port := web.DefaultPort
	if viper.IsSet("web_port") {
		port = viper.GetInt("web_port")
	}
	if flagPort, err := cmd.Flags().GetInt("web-port"); err == nil && flagPort >= 0 {
		port = flagPort
	}
	return host, port
}

// RunStart is the shared handler for `pm2 daemon start` and `pm2 start`.
func RunStart(cmd *cobra.Command, _ []string) error {
	foreground, err := cmd.Flags().GetBool("foreground")
	if err != nil {
		return fmt.Errorf("read --foreground: %w", err)
	}
	if foreground {
		srv := daemonruntime.NewServer(cliruntime.PM2Home())
		srv.WebHost, srv.WebPort = WebConfig(cmd)
		err := srv.Listen(cliruntime.SocketPath())
		if errors.Is(err, daemonruntime.ErrAlreadyRunning) {
			// Exit 0, deliberately. The request was "make sure a daemon
			// is running" and one is — losing the singleton race is the
			// guard working, not a failure. A supervisor restarts what
			// exits non-zero, so reporting failure here would put
			// launchd's KeepAlive (and systemd's Restart=on-failure)
			// into a permanent retry loop against a socket it can never
			// own.
			fmt.Println("PM2 daemon is already running.")
			return nil
		}
		return err
	}
	host, port := WebConfig(cmd)
	return startAsBackground(host, port)
}

// startAsBackground re-execs the current binary with
// `daemon start --foreground` and detaches it. Stdout/stderr are
// redirected to `~/.config/pm2/logs/daemon.log` /
// `~/.config/pm2/logs/daemon-err.log` so
// the user can `tail -f` them after the fact. Setpgid ensures the
// daemon is its own process group leader (so `pm2 daemon kill` can
// later signal the whole tree if needed).
func startAsBackground(webHost string, webPort int) error {
	// A daemon is a singleton per socket. Without this check the
	// spawn "succeeds" and prints a start message while the child
	// immediately dies on the listener's own singleton guard —
	// reporting a start that never happened.
	if resp, err := model.SendRequest(
		cliruntime.SocketPath(),
		model.Request{Command: model.CmdPing},
	); err == nil && resp.OK {
		fmt.Println("PM2 daemon is already running.")
		return nil
	}

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

	logDir := cliruntime.DaemonLogsDir()
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

	// The web flags have to travel into the re-exec argv. The child
	// re-reads viper on its own, but a flag the user typed here would
	// otherwise be silently dropped by the detach.
	cmd := exec.Command(exe, "daemon", "start", "--foreground",
		"--web-host", webHost, "--web-port", strconv.Itoa(webPort))
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
