package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
)

// save serialises the in-memory process map to <homeDir>/dump.json.
// The RLock is taken inside ProcessRegistry.SnapshotAppConfigs so
// callers do not need to lock — this prevents the "concurrent map
// iteration and map write" fatal when startAutoSave (background tick)
// and CmdSave (RPC) race against launchProcess / stopProcess.
//
// File I/O runs outside the lock so a slow disk write does not block
// other RPC handlers or the cron / watch goroutines.
//
// dump.json is now []process.AppConfig directly — the unified-config
// refactor (Phase 4) eliminated the separate DumpEntry type. Each
// process contributes its embedded AppConfig (everything except the
// runtime fields: ID, PID, Status, Restarts, StartedAt, CPU, Memory,
// User, LastCronAt, LastCronStatus).
func (s *Server) save() error {
	entries := s.reg.SnapshotAppConfigs()

	dumpPath := filepath.Join(s.homeDir, "dump.json")
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dumpPath, data, 0o644)
}

// resurrect reads <homeDir>/dump.json and starts every saved process.
// A per-entry failure is logged but does not abort the rest.
//
// The dump format is []process.AppConfig. Resurrect also calls
// AppConfig.Normalize() on each entry so the defaults for MaxRestarts,
// Instances, Name derivation, etc. match what `pm2 start` from a fresh
// ecosystem file would produce — closing the gap that
// plans/architecture-unified-config.md §1.3 flagged as a known bug.
func (s *Server) resurrect() error {
	dumpPath := filepath.Join(s.homeDir, "dump.json")
	data, err := os.ReadFile(dumpPath)
	if err != nil {
		return fmt.Errorf("no dump found (run pm2 save first): %w", err)
	}
	var entries []process.AppConfig
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("dump.json format incompatible (unified-config refactor — please run `pm2 delete all` then re-add your apps, or restore from a pre-refactor backup): %w", err)
	}
	for i := range entries {
		entries[i].Normalize("")
		req := &model.AppStartReq{AppConfig: entries[i]}
		if _, err := s.startApp(req); err != nil {
			slog.Info("resurrect error", "name", entries[i].Name, "err", err)
		}
	}
	return nil
}
