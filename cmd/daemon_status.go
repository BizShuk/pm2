package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
	"github.com/spf13/cobra"
)

// newDaemonStatusCmd returns `pm2 daemon status`.
//
// Semantics: send `model.CmdStatus` to the running daemon; on success
// the daemon replies with a `process.DaemonInfo` payload (PID, started
// time, version, home, process count) that this command formats into
// the multi-line block shown below. On dial failure the command falls
// back to the "not running" message — same idempotent shape as
// `pm2 daemon kill` so the verb is always safe to run.
//
// This verb is read-only. It operates on the DAEMON's own metadata,
// not on any managed process.
func newDaemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the running PM2 daemon's identity and counters",
		Long: "Sends CmdStatus to the running daemon and renders its reply " +
			"as a multi-line summary: status, pid, started_at, uptime, version, " +
			"home_dir, socket_path, and managed-process count.\n\n" +
			"If the daemon is unreachable (no socket at the expected path), " +
			"the command prints a 'not running' notice with the socket path " +
			"and a hint to start it. This verb never mutates state.",
		Args: cobra.NoArgs,
		RunE: runDaemonStatus,
	}
}

// runDaemonStatus is the RunE body for `pm2 daemon status`. Behaviour:
//
//   - Daemon reachable → decode the CmdStatus payload and print the
//     formatted summary.
//   - Daemon unreachable → treat as "not running" and print a friendly
//     message + socket path + start hint, return nil (idempotent).
//
// The command never returns an error for the unreachable case; that
// matches `pm2 daemon kill`'s contract and keeps the verb safe for
// shell scripts that probe the daemon before launching.
func runDaemonStatus(cmd *cobra.Command, args []string) error {
	sock := socketPath()

	resp, err := model.SendRequest(sock, model.Request{Command: model.CmdStatus})
	if err != nil {
		// No reachable daemon — treat as a no-op.
		fmt.Println("PM2 daemon is not running.")
		fmt.Printf("  socket:      %s\n", sock)
		fmt.Println("  Start with:  pm2 daemon start")
		return nil
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}

	var info process.DaemonInfo
	if err := json.Unmarshal(resp.Payload, &info); err != nil {
		return fmt.Errorf("decode status payload: %w", err)
	}

	uptime := time.Since(info.StartedAt).Truncate(time.Second)
	uptimeStr := uptime.String()

	fmt.Println("PM2 daemon")
	fmt.Printf("  status:      running\n")
	fmt.Printf("  pid:         %d\n", info.PID)
	fmt.Printf("  started:     %s (%s ago)\n",
		info.StartedAt.Format("2006-01-02 15:04:05"), uptimeStr)
	fmt.Printf("  uptime:      %s\n", uptimeStr)
	fmt.Printf("  version:     %s\n", info.Version)
	fmt.Printf("  home:        %s\n", info.HomeDir)
	fmt.Printf("  socket:      %s\n", sock)
	fmt.Printf("  processes:   %d\n", info.ProcessCount)
	return nil
}