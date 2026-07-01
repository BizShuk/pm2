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
// The RLock is taken internally so callers do not need to lock — this
// matches listAll / findProcesses and prevents the "concurrent map
// iteration and map write" fatal when startAutoSave (background tick)
// and CmdSave (RPC) race against launchProcess / stopProcess.
//
// File I/O runs outside the lock so a slow disk write does not block
// other RPC handlers or the cron / watch goroutines.
func (s *Server) save() error {
	s.mu.RLock()
	entries := make([]process.DumpEntry, 0, len(s.processes))
	for _, mp := range s.processes {
		entries = append(entries, process.DumpEntry{
			Namespace:   mp.Info.Namespace,
			Name:        mp.Info.Name,
			Script:      mp.Info.Script,
			Args:        mp.Info.Args,
			Env:         mp.Info.Env,
			CronRestart: mp.Info.CronRestart,
			Cron:        mp.Info.Cron,
			Instances:   1,
			MaxRestarts: mp.Info.MaxRestarts,
			LogFile:     mp.Info.LogFile,
			OutFile:     mp.Info.LogFile,
			ErrorFile:   mp.Info.ErrorFile,
			ConfigDir:   mp.Info.ConfigDir,
			Watch:       mp.Info.Watch,
			Version:     mp.Info.Version,
			ConfigFile:  mp.Info.ConfigFile,
			CWD:         mp.Info.CWD,
			BaseEnv:     mp.Info.BaseEnv,
		})
	}
	s.mu.RUnlock()

	dumpPath := filepath.Join(s.homeDir, "dump.json")
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dumpPath, data, 0o644)
}

// resurrect reads <homeDir>/dump.json and starts every saved process.
// A per-entry failure is logged but does not abort the rest.
func (s *Server) resurrect() error {
	dumpPath := filepath.Join(s.homeDir, "dump.json")
	data, err := os.ReadFile(dumpPath)
	if err != nil {
		return fmt.Errorf("no dump found (run pm2 save first): %w", err)
	}
	var entries []process.DumpEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	for _, e := range entries {
		req := &model.AppStartReq{
			Namespace:   e.Namespace,
			Name:        e.Name,
			Script:      e.Script,
			Args:        e.Args,
			Env:         e.Env,
			CronRestart: e.CronRestart,
			Cron:        e.Cron,
			Watch:       e.Watch,
			Instances:   e.Instances,
			MaxRestarts: e.MaxRestarts,
			LogFile:     e.LogFile,
			OutFile:     e.OutFile,
			ErrorFile:   e.ErrorFile,
			ConfigDir:   e.ConfigDir,
			Version:     e.Version,
			ConfigFile:  e.ConfigFile,
			CWD:         e.CWD,
			BaseEnv:     e.BaseEnv,
		}
		if _, err := s.startApp(req); err != nil {
			slog.Info("resurrect error", "name", e.Name, "err", err)
		}
	}
	return nil
}