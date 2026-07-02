package executor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ProcessSample is a per-process snapshot taken under the registry's
// read lock so the slow `ps` calls (phase 2) can run without holding
// the lock. Defined here so the executor package owns its own types.
type ProcessSample struct {
	PID    int
	Online bool // true when PID > 0 and Status is Online at snapshot time
}

// MetricsBackend is the read/write surface the collector needs from
// the ProcessRegistry. Defined as an interface so tests can mock it
// without spinning up a real registry.
type MetricsBackend interface {
	// SnapshotForMetrics returns (key -> ProcessSample) under a read lock.
	// The returned map is a fresh allocation; the collector mutates the
	// sample struct in phase 2 (no shared write needed).
	SnapshotForMetrics() map[string]ProcessSample
	// UpdateMetrics writes (cpu, mem) onto the process stored under key,
	// but only when its PID still matches expectedPID. Returns true on write.
	UpdateMetrics(key string, expectedPID int, cpu float64, mem uint64) bool
}

// metricsSample is one (cpu, mem) reading for a snapshot target.
type metricsSample struct {
	cpu float64
	mem uint64
}

// MetricsWorkers bounds the parallelism of phase 2. `ps` is
// fork+exec+wait-bound (mostly waiting on the kernel), so we can have
// more workers than logical CPUs without CPU pressure. Empirically,
// 8 workers keeps a 30-process sweep under ~50 ms wall-clock while
// leaving headroom for the rest of the daemon.
const MetricsWorkers = 8

// MetricsCollector owns the three-phase refresh loop. The pipeline:
//
//	1. RLock     — snapshot (key, pid, online) per process.
//	2. UNLOCKED  — call `ps` in parallel via a bounded worker pool.
//	3. Lock      — write the samples back to the matching ProcessInfo.
//
// Phase 2 also re-checks (a) the key still exists in the map and
// (b) the PID still matches the snapshot — that's the backend's job
// (UpdateMetrics re-checks both).
type MetricsCollector struct {
	backend MetricsBackend
	workers int
}

// NewMetricsCollector returns a collector reading from backend and
// running phase 2 with at most `workers` parallel goroutines.
func NewMetricsCollector(backend MetricsBackend, workers int) *MetricsCollector {
	return &MetricsCollector{backend: backend, workers: workers}
}

// Refresh performs one pass. Same algorithm as the prior Server.refreshMetrics.
func (c *MetricsCollector) Refresh() {
	// Phase 1 — snapshot under the backend's internal RLock.
	samples := c.backend.SnapshotForMetrics()
	targets := make([]struct {
		key    string
		pid    int
		online bool
	}, 0, len(samples))
	for k, s := range samples {
		targets = append(targets, struct {
			key    string
			pid    int
			online bool
		}{key: k, pid: s.PID, online: s.Online})
	}

	// Phase 2 — collect metrics in parallel. NO lock held here.
	out := make([]metricsSample, len(targets))
	if n := len(targets); n > 0 {
		work := make(chan int, n)
		workers := c.workers
		if workers > n {
			workers = n
		}
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Go(func() {
				for i := range work {
					if t := targets[i]; t.online {
						out[i].cpu, out[i].mem = GetProcessMetrics(t.pid)
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

	// Phase 3 — writeback. UpdateMetrics holds the write lock
	// internally and re-checks both the key and the PID.
	for i, t := range targets {
		c.backend.UpdateMetrics(t.key, t.pid, out[i].cpu, out[i].mem)
	}
}

// Run loops Refresh every 2 seconds until ctx is cancelled.
func (c *MetricsCollector) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.Refresh()
		}
	}
}

// GetProcessMetrics reads %CPU and RSS bytes for pid via `ps`.
// Returns (0, 0) for missing or unreadable processes.
//
// Exported as a package-level var so tests can swap in a slow / fake
// implementation without spinning up real OS processes (each `ps` is
// a fork+exec+wait — ~5-50 ms per call, which is the whole reason the
// three-phase refresh exists in the first place).
//
// Tests stub via:
//
//	orig := executor.GetProcessMetrics
//	executor.GetProcessMetrics = func(pid int) (float64, uint64) { ... }
//	defer func() { executor.GetProcessMetrics = orig }()
var GetProcessMetrics = func(pid int) (float64, uint64) {
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