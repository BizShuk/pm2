package daemon

import "log/slog"

// autoSave persists the registry to dump.json immediately after any task
// operation issued through the CLI: start, restart, stop, pause, resume,
// and delete. Without it the dump only caught up on the 10-minute
// auto-save tick, so a daemon restart in between replayed a stale world —
// a deleted task came back, a paused task resumed itself.
//
// It is deliberately best-effort and never fails the RPC that triggered it:
// the operation has already happened to the real process, and reporting a
// persistence error as an operation failure would be a lie about what
// happened. Errors are logged with the operation that caused them.
//
// The internal restart paths (cron fire, file-watch trigger) deliberately
// do NOT call this — see RestartByName / restartTargets.
func (pm *ProcessManager) autoSave(operation string) {
	if pm.suppressAutoSave.Load() {
		return
	}
	if err := pm.Save(); err != nil {
		slog.Error("auto-save after registry change failed",
			"operation", operation,
			"home_dir", pm.homeDir,
			"process_count", pm.reg.Len(),
			"err", err)
		return
	}
	slog.Info("auto-save: registry persisted", "operation", operation)
}
