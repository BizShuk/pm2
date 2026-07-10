// ProcessRegistry is the *sole* owner of the in-memory process map and
// its RWMutex. Before Phase 3 those lived as bare fields on daemon.Server
// (Server.mu sync.RWMutex and Server.processes map[string]*ManagedProcess);
// 155 call sites across the daemon package took those locks and walked the
// map directly. That scattered the responsibility for "where state lives"
// and made the locking invariants impossible to reason about locally.
//
// After this refactor every map mutation goes through a ProcessRegistry
// method, every field mutation that races with watchProcess's goroutine
// goes through UpdateInfo / UpdateMetrics / UpdateCronStatus, and the only
// callers that touch sync.RWMutex primitives are the registry itself plus
// the lock-delegate methods on Server (so existing tests that did
// s.mu.RLock() continue to compile).
package daemon

import (
	"fmt"
	"sync"
	"time"

	"github.com/bizshuk/pm2/daemon/executor"
	"github.com/bizshuk/pm2/process"
)

// ProcessRegistry encapsulates the in-memory process map together with the
// RWMutex that protects it. All exported methods are safe for concurrent
// use; callers must NOT hold the lock when calling any of them (the methods
// take the lock internally).
//
// Lock direction: ProcessRegistry is the *only* holder of the underlying
// sync.RWMutex. daemon.Server does not own a separate lock for this map.
// If a caller needs to do a multi-step read-modify-write that spans more
// than one method call, they should use the Lock/RLock escape hatches —
// not reimplement the lock elsewhere.
type ProcessRegistry struct {
	mu        sync.RWMutex
	processes map[string]*ManagedProcess
}

// NewProcessRegistry returns an empty, ready-to-use registry.
func NewProcessRegistry() *ProcessRegistry {
	return &ProcessRegistry{processes: make(map[string]*ManagedProcess)}
}

// Add stores mp under key. If key already exists it is replaced; this
// matches the prior direct-map-assignment semantics in launchProcess.
//
// Add is idempotent. Callers that want "add only if absent" must do their
// own Get first.
func (r *ProcessRegistry) Add(key string, mp *ManagedProcess) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.processes[key] = mp
}

// Get returns the ManagedProcess stored under key. The second return is
// false if the key is absent. The returned pointer is the live pointer —
// callers must not mutate fields without first taking the lock (use
// UpdateInfo for that).
func (r *ProcessRegistry) Get(key string) (*ManagedProcess, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	mp, ok := r.processes[key]
	return mp, ok
}

// SnapshotOne returns a value-copy of the ProcessInfo stored under key.
// The copy is taken under the read lock so the snapshot is atomic with
// respect to any UpdateInfo / UpdateMetrics / UpdateCronStatus write;
// callers may freely read fields from the returned value without holding
// any registry lock.
//
// Returns (zero, false) if the key is absent.
//
// This is the read-side counterpart to the CLAUDE.md §Conventions rule
// that field mutations on a single *ManagedProcess must go through
// UpdateInfo. Just as a naked `mp.Info.X = ...` races with onProcessExit,
// so does a naked `mp.Info.X` read — direct field reads from outside the
// registry are forbidden regardless of direction. SnapshotOne is the
// sanctioned read path for test code and the rare read-only consumer
// that needs a single ProcessInfo without Snapshot()'s full map copy.
func (r *ProcessRegistry) SnapshotOne(key string) (process.ProcessInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	mp, ok := r.processes[key]
	if !ok {
		return process.ProcessInfo{}, false
	}
	return mp.Info, true
}

// Remove deletes the entry under key. It returns the removed mp and true
// if a removal happened, or (nil, false) if the key was absent.
//
// Used by deleteByName and the (future) Executor Stop path.
func (r *ProcessRegistry) Remove(key string) (*ManagedProcess, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mp, ok := r.processes[key]
	if !ok {
		return nil, false
	}
	delete(r.processes, key)
	return mp, true
}

// List returns a snapshot slice of every ManagedProcess. The slice is a
// fresh allocation; the mp pointers are the live pointers, so callers must
// not mutate mp fields without taking the lock.
func (r *ProcessRegistry) List() []*ManagedProcess {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.listLocked()
}

// SnapshotMap returns a snapshot copy of the entire (key → mp) map. The
// map is a fresh allocation; the mp values are the live pointers. This
// is the right shape for code that needs to iterate both keys and values
// (e.g. metrics collector phase 1, tests that range over the map).
func (r *ProcessRegistry) SnapshotMap() map[string]*ManagedProcess {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]*ManagedProcess, len(r.processes))
	for k, v := range r.processes {
		out[k] = v
	}
	return out
}

// SnapshotForMetrics returns a (key -> executor.ProcessSample) map under
// the read lock. Used by executor.MetricsCollector.Refresh (phase 1).
// Implements executor.MetricsBackend.
//
// ProcessSample deliberately hides the full *ManagedProcess from the
// executor package — the collector only needs (PID, Online) for its
// three-phase pipeline. Caller MUST treat the returned map as a
// snapshot; the mp pointers are not exposed so no field mutation can
// leak back into the registry from the executor side.
func (r *ProcessRegistry) SnapshotForMetrics() map[string]executor.ProcessSample {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]executor.ProcessSample, len(r.processes))
	for k, mp := range r.processes {
		out[k] = executor.ProcessSample{
			PID:    mp.Info.PID,
			Online: mp.Info.PID > 0 && mp.Info.Status == process.StatusOnline,
		}
	}
	return out
}

// Snapshot returns a copy of every process's ProcessInfo — same shape that
// daemon.Server.listAll used to return. Used by pm2 list RPC handler and
// by persistence.save.
func (r *ProcessRegistry) Snapshot() []process.ProcessInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]process.ProcessInfo, 0, len(r.processes))
	for _, mp := range r.processes {
		out = append(out, mp.Info)
	}
	return out
}

// SnapshotAppConfigs returns the AppConfig half of every ProcessInfo — the
// persistent fields. Used by persistence.save (dump.json serialisation).
//
// The Paused bit is copied from ManagedProcess.paused onto each emitted
// AppConfig so that `pm2 pause` survives save/resurrect. Without this hook
// (regression test: TestPausedCronTaskSurvivesResurrect), a cron task that
// was deliberately suspended would come back with its scheduler entry
// re-registered and fire on schedule after a daemon restart.
func (r *ProcessRegistry) SnapshotAppConfigs() []process.AppConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]process.AppConfig, 0, len(r.processes))
	for _, mp := range r.processes {
		cfg := mp.Info.AppConfig
		cfg.Paused = mp.paused
		out = append(out, cfg)
	}
	return out
}

// Len returns the number of registered processes.
func (r *ProcessRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.processes)
}

// FindByTarget replicates the four-tier matching logic that used to live in
// daemon.findProcesses:
//
//	0. exact "namespace:name" key match (so cron callbacks with composite
//	   keys resolve to the correct process across namespaces)
//	1. numeric ID match
//	2. plain name match (returns all matches across namespaces)
//	3. namespace match (returns every process in that namespace)
//	"all" is a special target that returns every registered process.
func (r *ProcessRegistry) FindByTarget(target string) []*ManagedProcess {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if target == "all" {
		out := make([]*ManagedProcess, 0, len(r.processes))
		for _, mp := range r.processes {
			out = append(out, mp)
		}
		return out
	}

	// 0. exact key match
	if mp, ok := r.processes[target]; ok {
		return []*ManagedProcess{mp}
	}

	// 1. ID match
	var idVal int
	if _, err := fmt.Sscan(target, &idVal); err == nil {
		for _, mp := range r.processes {
			if mp.Info.ID == idVal {
				return []*ManagedProcess{mp}
			}
		}
	}

	// 2. name match (all namespaces)
	var matchedByName []*ManagedProcess
	for _, mp := range r.processes {
		if mp.Info.Name == target {
			matchedByName = append(matchedByName, mp)
		}
	}
	if len(matchedByName) > 0 {
		return matchedByName
	}

	// 3. namespace match
	var matchedByNS []*ManagedProcess
	for _, mp := range r.processes {
		if mp.Info.Namespace == target {
			matchedByNS = append(matchedByNS, mp)
		}
	}
	return matchedByNS
}

// UpdateInfo runs fn under the write lock, atomically applying whatever
// mutations fn performs to the mp stored under key. If the key is absent,
// fn is *not* invoked and false is returned.
//
// IMPORTANT: fn must NOT call back into the same ProcessRegistry (no
// Get/Add/Remove/UpdateInfo from inside fn) — that would re-enter the
// lock and deadlock. fn should be a pure field mutator.
func (r *ProcessRegistry) UpdateInfo(key string, fn func(*ManagedProcess)) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	mp, ok := r.processes[key]
	if !ok {
		return false
	}
	fn(mp)
	return true
}

// UpdateMetrics writes (cpu, mem) onto the mp stored under key, but only
// when the mp's current PID still matches expectedPID. This guards the
// metrics collector against writing a stale sample onto a process that
// was restarted during the slow `ps` collection phase (see daemon/metrics.go
// refreshMetrics phase 3).
//
// Returns true if the write happened.
func (r *ProcessRegistry) UpdateMetrics(key string, expectedPID int, cpu float64, mem uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	mp, ok := r.processes[key]
	if !ok || mp.Info.PID != expectedPID {
		return false
	}
	if mp.Info.PID > 0 && mp.Info.Status == process.StatusOnline {
		mp.Info.CPU = cpu
		mp.Info.Memory = mem
	} else {
		mp.Info.CPU = 0
		mp.Info.Memory = 0
	}
	return true
}

// UpdateCronStatus sets LastCronAt and LastCronStatus onto the mp stored
// under key. Used by launchProcess's cron callback and by triggerCron.
func (r *ProcessRegistry) UpdateCronStatus(key string, firedAt time.Time, status string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	mp, ok := r.processes[key]
	if !ok {
		return false
	}
	mp.Info.LastCronAt = firedAt
	mp.Info.LastCronStatus = status
	return true
}

// LookupExistingForLaunch is the launch-time duplicate detector used by
// Server.startApp / Server.launchProcess. It mirrors the original
// s.processes[k] + by-(Name,ConfigFile) scan in one atomic read-locked
// operation, and returns BOTH the matching mp AND its current key (so
// the caller can delete the old entry when the new key differs — e.g.
// when a process was started in a different namespace previously).
//
// Returns found=false if neither lookup matches.
func (r *ProcessRegistry) LookupExistingForLaunch(ns, name, configFile string) (mp *ManagedProcess, oldKey string, found bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.findExistingForLaunchUnderLock(ns, name, configFile)
}

// findExistingForLaunchUnderLock is the lock-held counterpart to
// LookupExistingForLaunch. Caller MUST hold r.mu (RLock or Lock).
// Used by launchProcess in a multi-step "compute id + write mp" section
// that needs exclusive access across the whole sequence.
func (r *ProcessRegistry) findExistingForLaunchUnderLock(ns, name, configFile string) (mp *ManagedProcess, oldKey string, found bool) {
	k := cronKey(ns, name)
	if m, ok := r.processes[k]; ok {
		return m, k, true
	}
	if configFile != "" {
		for k, m := range r.processes {
			if m.Info.Name == name && m.Info.ConfigFile == configFile {
				return m, k, true
			}
		}
	}
	return nil, "", false
}

// Lock / Unlock / RLock / RUnlock are escape hatches for callers that need
// to hold the registry's lock across more than one method call. Prefer the
// high-level methods (Get/Add/UpdateInfo/...) for one-shot operations.
//
// Examples of legitimate escape-hatch use:
//   - daemon/server.go::startApp: needs to do a Get + a multi-condition
//     check + a stopProcess + a re-Get, all without another writer
//     slipping a conflicting entry between the steps.
//   - daemon/metrics.go::refreshMetrics phase 1: needs to iterate the
//     whole map and snapshot (key, pid, online) tuples atomically.
//
// IMPORTANT: do NOT call any ProcessRegistry method while holding these
// locks — that re-enters the mutex and deadlocks.
func (r *ProcessRegistry) Lock()    { r.mu.Lock() }
func (r *ProcessRegistry) Unlock()  { r.mu.Unlock() }
func (r *ProcessRegistry) RLock()   { r.mu.RLock() }
func (r *ProcessRegistry) RUnlock() { r.mu.RUnlock() }

// listLocked is the body of List that runs under the read lock. Split out
// so UpdateInfo-style wrappers can re-use the iteration pattern without
// re-locking.
func (r *ProcessRegistry) listLocked() []*ManagedProcess {
	out := make([]*ManagedProcess, 0, len(r.processes))
	for _, mp := range r.processes {
		out = append(out, mp)
	}
	return out
}