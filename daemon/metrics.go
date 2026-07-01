package daemon

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/bizshuk/pm2/process"
)

// getProcessMetrics reads %CPU and RSS bytes for pid via `ps`.
// Returns (0, 0) for missing or unreadable processes.
//
// Exposed as a package-level var so tests can swap in a slow / fake
// implementation without spinning up real OS processes (each `ps` is
// a fork+exec+wait — ~5-50ms per call, which is the whole reason the
// three-phase refresh exists in the first place).
var getProcessMetrics = func(pid int) (float64, uint64) {
	if pid <= 0 {
		return 0, 0
	}
	out, err := exec.Command("ps", "-p", fmt.Sprintf("%d", pid), "-o", "%cpu,rss").Output()
	if err != nil {
		return 0, 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, 0
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 2 {
		return 0, 0
	}
	var cpu float64
	var rss uint64
	_, _ = fmt.Sscanf(fields[0], "%f", &cpu)
	_, _ = fmt.Sscanf(fields[1], "%d", &rss)
	return cpu, rss * 1024
}

// metricsTarget is a per-process snapshot taken under the read lock so
// the slow `ps` calls can run without holding s.mu.
type metricsTarget struct {
	key    string // map key (namespace:name)
	pid    int
	online bool   // was the process online at snapshot time?
}

// metricsSample is one (cpu, mem) reading for a snapshot target.
type metricsSample struct {
	cpu float64
	mem uint64
}

// metricsWorkers bounds the parallelism of phase 2. `ps` is
// fork+exec+wait-bound (mostly waiting on the kernel), so we can have
// more workers than logical CPUs without CPU pressure. Empirically,
// 8 workers keeps a 30-process sweep under ~50 ms wall-clock while
// leaving headroom for the rest of the daemon.
const metricsWorkers = 8

// refreshMetrics does one pass of CPU/Memory collection for every
// managed process. The pipeline is split into three explicit phases:
//
//   1. RLock     — snapshot the (key, pid, online) tuple per process.
//   2. UNLOCKED  — call `ps` in parallel via a bounded worker pool.
//   3. Lock      — write the samples back to the matching ProcessInfo.
//
// This means pm2 list / pm2 save / pm2 stop can proceed during step 2
// instead of being blocked behind N fork+exec+wait calls. Previously,
// the for-loop held s.mu.Lock() for the entire collection, freezing
// every RPC for hundreds of milliseconds.
//
// Phase 3 also re-checks (a) the key still exists in the map and
// (b) the PID still matches the snapshot. Without (b), a process that
// was restarted during the slow phase would inherit the old PID's
// stale CPU/Memory readings until the next 2 s tick.
func (s *Server) refreshMetrics() {
	// Phase 1 — snapshot under RLock.
	s.mu.RLock()
	targets := make([]metricsTarget, 0, len(s.processes))
	for k, mp := range s.processes {
		targets = append(targets, metricsTarget{
			key:    k,
			pid:    mp.Info.PID,
			online: mp.Info.PID > 0 && mp.Info.Status == process.StatusOnline,
		})
	}
	s.mu.RUnlock()

	// Phase 2 — collect metrics in parallel. NO lock held here; this
	// is the slow path that previously starved every concurrent RPC.
	// A bounded worker pool overlaps the fork+exec+wait calls so
	// wall-clock cost is roughly total / workers instead of total / 1.
	//
	// Each worker writes to a distinct index in `samples`, so the
	// slice itself needs no extra synchronisation (distinct slots
	// of a pre-allocated array are race-free under Go's memory model).
	samples := make([]metricsSample, len(targets))
	if n := len(targets); n > 0 {
		work := make(chan int, n)
		workers := metricsWorkers
		if workers > n {
			workers = n
		}
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Go(func() {
				for i := range work {
					if t := targets[i]; t.online {
						samples[i].cpu, samples[i].mem = getProcessMetrics(t.pid)
					}
				}
			})
		}
		for i := range targets {
			work <- i
		}
		close(work)
		wg.Wait()
	}

	// Phase 3 — write back under Lock. A process may have been
	// stopped/deleted between snapshot and write-back; a process may
	// also have been restarted (new PID). Both cases skip the write.
	s.mu.Lock()
	for i, t := range targets {
		mp, ok := s.processes[t.key]
		if !ok || mp.Info.PID != t.pid {
			continue
		}
		if t.online {
			mp.Info.CPU = samples[i].cpu
			mp.Info.Memory = samples[i].mem
		} else {
			mp.Info.CPU = 0
			mp.Info.Memory = 0
		}
	}
	s.mu.Unlock()
}

// StartMetricsCollector spawns a goroutine that refreshes CPU/Mem on
// every process every 2 seconds. Runs until the daemon exits.
func (s *Server) StartMetricsCollector() {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			s.refreshMetrics()
		}
	}()
}