package daemon

import (
	"fmt"

	"github.com/bizshuk/pm2/process"
)

// listAll returns a snapshot of every managed process's ProcessInfo.
// Caller does not need to lock; ProcessRegistry.Snapshot takes the read
// lock internally.
func (s *Server) listAll() []process.ProcessInfo {
	return s.reg.Snapshot()
}

// findProcesses resolves a target string to one or more managed processes.
// Matching priority: numeric ID > exact name > namespace > "all" (special).
// Caller does not need to lock; ProcessRegistry.FindByTarget takes the
// read lock internally.
func (s *Server) findProcesses(target string) []*ManagedProcess {
	return s.reg.FindByTarget(target)
}

// deleteByName removes every matching process from the registry.
// The process is stopped first; the registry entry is then removed.
func (s *Server) deleteByName(name string) error {
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