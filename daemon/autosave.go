package daemon

import "log/slog"

// autoSave persists the registry to dump.json immediately after a change
// in registry membership (an app registered or removed). Without it the
// dump only caught up on the 10-minute auto-save tick, so a daemon restart
// in between could resurrect a deleted task or lose a newly added one.
//
// It is deliberately best-effort and never fails the RPC that triggered it:
// the process is already started (or already gone), and reporting a
// persistence error as a start/delete failure would be a lie about what
// happened. Errors are logged with the operation that caused them.
//
// Restart, pause, and resume do not call this — they change a process's
// state, not the set of registered processes. Those still ride the
// periodic auto-save.
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
