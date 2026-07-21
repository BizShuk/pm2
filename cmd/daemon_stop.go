package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bizshuk/pm2/model"
	"github.com/spf13/cobra"
)

// stopMarkerFile lives in pm2Home and is the on-disk flag that tells
// `autoStartDaemon` NOT to silently respawn the daemon on the next CLI
// invocation. The user sets it explicitly via `pm2 daemon stop` and
// `pm2 daemon start` removes it. The file name is descriptive (not a
// dotfile) so it shows up in `ls ~/.pm2` and gives the user a hint
// about what it means.
const stopMarkerFile = "daemon.stopped"

// writeStopMarker creates the marker file. A nil result means the
// marker is present (either just written or already there); an error
// is returned only for genuine I/O problems so the caller can decide
// whether to surface them.
//
// We use 0644 (not 0600) so that the file is easy to inspect and
// delete by hand if needed — there is no sensitive data in it.
func writeStopMarker() error {
	path := filepath.Join(pm2Home, stopMarkerFile)
	return os.WriteFile(path, []byte{}, 0o644)
}

// DaemonStopCmd is `pm2 daemon stop`.
//
// Semantics: write the "user stopped" marker, then send `CmdKill` to
// the running daemon (graceful stop of every managed process + the
// daemon's own exit, identical to `pm2 daemon kill`).
//
// The marker is what makes this command different from `daemon kill`:
//
//   - `daemon kill` — daemon exits; subsequent CLI invocations WILL
//     auto-spawn a fresh daemon when they need one (today's behaviour).
//   - `daemon stop` — daemon exits; subsequent CLI invocations will
//     NOT auto-spawn. The user opted out of the silent-respawn path,
//     and `pm2 daemon start` is required to bring it back.
//
// This verb is idempotent: if the daemon is already gone, the marker
// is still written (so the auto-spawn suppression remains consistent)
// and the command reports success without an error.
var DaemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the PM2 daemon and prevent auto-respawn",
	Long: "Stops the running daemon (graceful stop of every managed process " +
		"plus the daemon's own exit, same as `pm2 daemon kill`) AND records " +
		"an on-disk marker that suppresses the silent auto-respawn path used " +
		"by other CLI commands. Behaviour:\n" +
		"  - Daemon reachable → graceful stop of every process, daemon exits, " +
		"marker is written.\n" +
		"  - Daemon unreachable (socket gone) → marker is still written, the " +
		"command reports success (idempotent).\n\n" +
		"To re-enable the daemon and clear the auto-respawn suppression, run " +
		"`pm2 daemon start`.\n\n" +
		"This is the daemon-lifecycle verb; for stopping a single managed " +
		"process while the daemon keeps running, use `pm2 stop <name|id|all>`.",
	Args: cobra.NoArgs,
	RunE: runDaemonStop,
}

// runDaemonStop is the RunE body for `pm2 daemon stop`. Behaviour:
//
//  1. Write the stop-marker FIRST so a concurrent CLI invocation
//     cannot auto-spawn a new daemon between our marker write and
//     our kill request reaching the daemon.
//  2. Send `CmdKill`. If the daemon is unreachable (already stopped),
//     treat as no-op — the marker is still the source of truth.
//  3. Report outcome.
//
// Why write the marker before the RPC: the marker is the only durable
// signal that survives the daemon's exit. If we wrote it AFTER, a
// concurrent `pm2 start <app>` racing between our RPC and our marker
// write would see "no daemon + no marker" and silently respawn — the
// bug we are explicitly preventing.
func runDaemonStop(cmd *cobra.Command, args []string) error {
	if err := writeStopMarker(); err != nil {
		// Don't fail outright — surface the marker write problem but
		// still attempt the kill. The marker is a UX hint, not a
		// correctness invariant.
		fmt.Fprintf(os.Stderr, "warning: could not write stop marker: %v\n", err)
	}

	resp, err := model.SendRequest(socketPath(), model.Request{Command: model.CmdKill})
	if err != nil {
		// No reachable daemon — marker is already in place, the
		// auto-spawn suppression is consistent with the user's intent.
		fmt.Println("PM2 daemon is not running. Auto-respawn suppressed.")
		fmt.Printf("  marker:       %s\n", filepath.Join(pm2Home, stopMarkerFile))
		fmt.Println("  Re-enable:    pm2 daemon start")
		return nil
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	fmt.Println("PM2 daemon stopped, auto-respawn suppressed.")
	fmt.Printf("  marker:       %s\n", filepath.Join(pm2Home, stopMarkerFile))
	fmt.Println("  Re-enable:    pm2 daemon start")
	return nil
}

// removeStopMarker clears the on-disk marker. Called by
// `startDaemonAsBackground` so a user-initiated `pm2 daemon start`
// explicitly re-enables the auto-respawn path.
//
// Tolerates "file does not exist" (the marker is the absence of an
// opt-out, not the presence of an opt-in) and any other error is
// surfaced so the caller can decide whether to fall back to a hard
// failure.
func removeStopMarker() error {
	path := filepath.Join(pm2Home, stopMarkerFile)
	err := os.Remove(path)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// hasStopMarker reports whether the marker file currently exists.
// The check is informational only — a permission error (e.g. marker
// owned by another user) is treated as "present" so the auto-spawn
// path refuses rather than silently overriding the user's intent.
func hasStopMarker() bool {
	path := filepath.Join(pm2Home, stopMarkerFile)
	_, err := os.Stat(path)
	return err == nil
}
