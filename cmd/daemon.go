package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// DaemonCmd is the `pm2 daemon` parent command with `start`,
// `kill`, `stop`, and `status` subcommands. Bare `pm2 daemon` errors
// out so the caller always picks an explicit verb; the internal
// auto-spawn paths use `daemon start --foreground` directly.
//
// The four subcommands live in their own files:
//
//   - daemon_start.go  — DaemonStartCmd + startDaemonAsBackground +
//     autoStartDaemon
//   - daemon_kill.go   — DaemonKillCmd + runDaemonKill
//   - daemon_stop.go   — DaemonStopCmd + runDaemonStop +
//     writeStopMarker / removeStopMarker / hasStopMarker
//   - daemon_status.go — DaemonStatusCmd + runDaemonStatus
var DaemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the PM2 daemon",
	Long: "Start or stop the PM2 daemon. Subcommands: start, kill, stop, status.\n" +
		"`pm2 daemon start` spawns the daemon in the background (or in\n" +
		"the foreground with --foreground). `pm2 daemon kill` asks the\n" +
		"running daemon to shut down all managed processes and exit\n" +
		"(subsequent CLI commands may still auto-respawn it).\n" +
		"`pm2 daemon stop` does the same teardown as `kill` AND writes a\n" +
		"marker that suppresses the silent auto-respawn path used by other\n" +
		"CLI commands. Run `pm2 daemon start` to clear the marker.\n" +
		"`pm2 daemon status` reports the daemon's PID, version, and\n" +
		"runtime counters (read-only; works whether or not the daemon\n" +
		"is currently running).",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("pm2 daemon requires a subcommand (start | kill | stop | status)")
	},
}

func init() {
	DaemonCmd.AddCommand(DaemonStartCmd, DaemonKillCmd, DaemonStopCmd, DaemonStatusCmd)
}
