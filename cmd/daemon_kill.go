package cmd

import (
	"fmt"

	"github.com/bizshuk/pm2/model"
	"github.com/spf13/cobra"
)

// newDaemonKillCmd returns `pm2 daemon kill`.
//
// Semantics: send `model.CmdKill` to the running daemon. The daemon
// invokes `KillAll()` (graceful stop of every managed process via
// the same `executor.Stop` path `pm2 stop` uses), then schedules
// `os.Exit(0)` from the RPC handler's post-response hook. See
// `daemon/network/handler.go` for the dispatcher side.
//
// This verb operates on the DAEMON, not on a managed process.
// For stopping an individual process while the daemon keeps running,
// use `pm2 stop <name|id|all>`.
func newDaemonKillCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kill",
		Short: "Stop all processes and shut down the PM2 daemon",
		Long: "Sends CmdKill to the running daemon, which gracefully stops every " +
			"managed process and then exits itself. Behaviour:\n" +
			"  - Daemon reachable → graceful stop of every process, then daemon exits.\n" +
			"  - Daemon unreachable (socket gone) → treated as a no-op.\n\n" +
			"This is the daemon-lifecycle verb; for stopping a single managed " +
			"process while the daemon keeps running, use `pm2 stop <name|id|all>` " +
			"instead.",
		Args: cobra.NoArgs,
		RunE: runDaemonKill,
	}
}

// runDaemonKill is the RunE body for `pm2 daemon kill`. Behaviour:
//
//   - Daemon reachable → send CmdKill, report outcome.
//   - Daemon unreachable (socket gone) → treat as "nothing to kill",
//     print a friendly message and return nil so this is idempotent.
//
// The post-response os.Exit(0) is scheduled by the daemon's RPC
// handler (not here), so a successful response implies the daemon
// is in the process of tearing itself down. The CLI does not need
// to wait or reconnect.
func runDaemonKill(cmd *cobra.Command, args []string) error {
	resp, err := model.SendRequest(socketPath(), model.Request{Command: model.CmdKill})
	if err != nil {
		// No reachable daemon — nothing to kill.
		fmt.Println("PM2 daemon is not running.")
		return nil
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	fmt.Println("PM2 daemon stopped, all processes killed.")
	return nil
}