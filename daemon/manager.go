package daemon

import (
	"fmt"

	"github.com/bizshuk/pm2/process"
)

// listAll returns a snapshot of every managed process's ProcessInfo.
// Caller does not need to lock; the RLock is taken internally.
func (s *Server) listAll() []process.ProcessInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	infos := make([]process.ProcessInfo, 0, len(s.processes))
	for _, mp := range s.processes {
		infos = append(infos, mp.Info)
	}
	return infos
}

// findProcesses resolves a target string to one or more managed processes.
// Matching priority: numeric ID > exact name > namespace > "all" (special).
// Caller does not need to lock; the RLock is taken internally.
func (s *Server) findProcesses(target string) []*ManagedProcess {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if target == "all" {
		var list []*ManagedProcess
		for _, mp := range s.processes {
			list = append(list, mp)
		}
		return list
	}

	// 1. ID 匹配
	var idVal int
	if _, err := fmt.Sscan(target, &idVal); err == nil {
		for _, mp := range s.processes {
			if mp.Info.ID == idVal {
				return []*ManagedProcess{mp}
			}
		}
	}

	// 2. Name 匹配
	var matchedByName []*ManagedProcess
	for _, mp := range s.processes {
		if mp.Info.Name == target {
			matchedByName = append(matchedByName, mp)
		}
	}
	if len(matchedByName) > 0 {
		return matchedByName
	}

	// 3. Namespace 匹配
	var matchedByNS []*ManagedProcess
	for _, mp := range s.processes {
		if mp.Info.Namespace == target {
			matchedByNS = append(matchedByNS, mp)
		}
	}
	return matchedByNS
}

// deleteByName removes every matching process from the map.
// The process is stopped first; the map entry is then removed under the lock.
func (s *Server) deleteByName(name string) error {
	targets := s.findProcesses(name)
	if len(targets) == 0 {
		return fmt.Errorf("process or namespace not found: %s", name)
	}
	for _, mp := range targets {
		_ = s.stopProcess(mp)
		s.mu.Lock()
		delete(s.processes, mp.Info.Namespace+":"+mp.Info.Name)
		s.mu.Unlock()
	}
	return nil
}