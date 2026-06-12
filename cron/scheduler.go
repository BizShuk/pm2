package cron

import (
	"sync"

	"github.com/robfig/cron/v3"
)

// Scheduler wraps robfig/cron with per-process entry tracking
type Scheduler struct {
	mu      sync.Mutex
	c       *cron.Cron
	entries map[string]cron.EntryID // keyed by process name
}

// New creates and starts a cron scheduler
func New() *Scheduler {
	s := &Scheduler{
		c:       cron.New(),
		entries: make(map[string]cron.EntryID),
	}
	s.c.Start()
	return s
}

// Register adds or replaces a cron job for the named process.
// fn is called whenever the schedule fires (typically process.Restart).
func (s *Scheduler) Register(name, schedule string, fn func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove existing entry for this process
	if id, ok := s.entries[name]; ok {
		s.c.Remove(id)
		delete(s.entries, name)
	}

	if schedule == "" {
		return nil
	}

	id, err := s.c.AddFunc(schedule, fn)
	if err != nil {
		return err
	}
	s.entries[name] = id
	return nil
}

// Remove cancels the cron job for the named process
func (s *Scheduler) Remove(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.entries[name]; ok {
		s.c.Remove(id)
		delete(s.entries, name)
	}
}

// Stop shuts down the scheduler gracefully
func (s *Scheduler) Stop() {
	s.c.Stop()
}
