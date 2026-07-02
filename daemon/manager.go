package daemon

import (
	"fmt"

	"github.com/bizshuk/pm2/process"
)

// ListAll returns a snapshot of every managed process's ProcessInfo.
// Caller does not need to lock; ProcessRegistry.Snapshot takes the read
// lock internally.
//
// Satisfies network.Manager (CmdList).
func (s *Server) ListAll() []process.ProcessInfo {
	return s.reg.Snapshot()
}

// findProcesses resolves a target string to one or more managed processes.
// Matching priority: numeric ID > exact name > namespace > "all" (special).
// Caller does not need to lock; ProcessRegistry.FindByTarget takes the
// read lock internally.
func (s *Server) findProcesses(target string) []*ManagedProcess {
	return s.reg.FindByTarget(target)
}

// DeleteByName removes every matching process from the registry.
// The process is stopped first; the registry entry is then removed.
//
// Satisfies network.Manager (CmdDelete).
func (s *Server) DeleteByName(name string) error {
	targets := s.findProcesses(name)
	if len(targets) == 0 {
		return fmt.Errorf("process or namespace not found: %s", name)
	}
	for _, mp := range targets {
		_ = s.stopProcess(mp)
		s.reg.Remove(mp.Info.Namespace + ":" + mp.Info.Name)
	}
	return nil
}

// Ping is the CmdPing handler. The network layer's dispatch already
// returns {OK:true} regardless; Ping exists so the Manager interface
// has a hook for future health checks (e.g. metrics counter read,
// uptime, etc.) without needing to change the interface.
//
// Satisfies network.Manager (CmdPing).
func (s *Server) Ping() {
	// No-op for now — the dispatcher returns OK without inspecting any
	// state. Override behavior here if a richer health check is added.
}