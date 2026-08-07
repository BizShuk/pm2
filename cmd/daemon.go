package cmd

import (
	"fmt"

	daemoncmd "github.com/bizshuk/pm2/cmd/daemon"
	"github.com/spf13/cobra"
)

// DaemonCmd is the `pm2 daemon` parent command with `start`,
// `kill`, `stop`, and `status` subcommands. Bare `pm2 daemon` errors
// out so the caller always picks an explicit verb; the internal
// auto-spawn paths use `daemon start --foreground` directly.
//
// Subcommands live in cmd/daemon/:
//
//   - start.go  — StartCmd + startAsBackground
//   - kill.go   — KillCmd + runKill
//   - stop.go   — StopCmd + runStop + marker management
//   - status.go — StatusCmd + runStatus
var DaemonCmd = &cobra.Command{
	Use:     "daemon",
	Aliases: []string{"d"},
	Short:   "Manage the PM2 daemon (short alias: pm2 d)",
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
	RunE: func(_ *cobra.Command, _ []string) error {
		return fmt.Errorf("pm2 daemon requires a subcommand (start | kill | stop | status)")
	},
}

func init() {
	DaemonCmd.AddCommand(
		daemoncmd.StartCmd,
		daemoncmd.KillCmd,
		daemoncmd.StopCmd,
		daemoncmd.StatusCmd,
	)
}
