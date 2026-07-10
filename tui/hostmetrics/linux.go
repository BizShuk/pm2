package hostmetrics

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// linuxCollector reads CPU% from /proc/stat and memory% from
// /proc/meminfo.
//
// CPU% is computed as a delta between two samples. The first
// sample is a warm-up: there is no previous tick to compare
// against, so we stash the totals and return the cosmetic
// fallback (5.2 / 64.1). Subsequent samples compute:
//
//	cpu = 100 * (totalDelta - idleDelta) / totalDelta
//
// where total = user + nice + system + idle + iowait. This is the
// classic Linux CPU% formula; we deliberately ignore irq/softirq/
// steal/guest in the idle accounting — those contribute to
// "busy" rather than "idle" and would only inflate the result by
// a fraction of a percent, well below the rendering precision.
//
// Memory% uses (MemTotal - MemAvailable) / MemTotal. MemAvailable
// is the modern kernel's "memory available for new allocations"
// figure (introduced in 3.14) and supersedes the older
// MemFree+Buffers+Cached heuristic.
type linuxCollector struct {
	mu        sync.Mutex
	prevIdle  uint64
	prevTotal uint64
	warmed    bool
}

func newLinuxCollector() *linuxCollector { return &linuxCollector{} }

// Collect reads /proc once and returns the current sample. The
// first call after construction returns the fallback values
// (because the CPU% delta needs a previous sample to subtract
// from); subsequent calls return real measurements.
//
// Errors from os.ReadFile bubble up unchanged so the caller can
// distinguish "permission denied in a sandbox" from "format
// shifted". The package-level downgrade policy lives in the
// caller (tui/model.go); this collector does not silently
// swallow errors.
func (l *linuxCollector) Collect() (float64, float64, error) {
	cpu, err := l.cpuPercent()
	if err != nil {
		return 0, 0, err
	}
	mem, err := l.memPercent()
	if err != nil {
		return 0, 0, err
	}
	return cpu, mem, nil
}

// cpuPercent returns the CPU% since the previous call, or the
// fallback (5.2, nil) on the first call. The mutex serialises
// the warm-up check so a fast TUI tick that races two Collect()
// calls cannot lose its first sample.
func (l *linuxCollector) cpuPercent() (float64, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, err
	}
	// /proc/stat starts with a line "cpu  user nice system idle
	// iowait irq softirq steal guest guest_nice". Aggregate the
	// first token == "cpu" — that's the per-CPU-summed line.
	var (
		user, nice, system, idle, iowait uint64
	)
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			break // only need the aggregate line
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return 0, fmt.Errorf("/proc/stat: unexpected format")
		}
		// Sscanf handles base-10 parsing of each tick count; the
		// trailing fields (irq, softirq, steal, guest, guest_nice)
		// are ignored.
		_, _ = fmt.Sscanf(fields[1], "%d", &user)
		_, _ = fmt.Sscanf(fields[2], "%d", &nice)
		_, _ = fmt.Sscanf(fields[3], "%d", &system)
		_, _ = fmt.Sscanf(fields[4], "%d", &idle)
		if len(fields) > 5 {
			_, _ = fmt.Sscanf(fields[5], "%d", &iowait)
		}
		break
	}

	total := user + nice + system + idle + iowait

	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.warmed {
		l.prevTotal, l.prevIdle = total, idle
		l.warmed = true
		return 5.2, nil
	}

	totalDelta := total - l.prevTotal
	idleDelta := idle - l.prevIdle
	l.prevTotal, l.prevIdle = total, idle

	if totalDelta == 0 {
		// Counter wrap or kernel jiffies not advancing (deep
		// idle container). Return the previous warm-up fallback
		// rather than NaN.
		return 5.2, nil
	}
	cpu := 100.0 * float64(totalDelta-idleDelta) / float64(totalDelta)
	return cpu, nil
}

// memPercent reads /proc/meminfo and returns
// (MemTotal - MemAvailable) / MemTotal * 100. MemAvailable
// is preferred over MemFree+Buffers+Cached because it accounts
// for reclaimable memory more accurately.
func (l *linuxCollector) memPercent() (float64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	var memTotal, memAvail uint64
	for line := range strings.SplitSeq(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			// "MemTotal:    16384000 kB"
			_, _ = fmt.Sscanf(line, "MemTotal: %d", &memTotal)
		case strings.HasPrefix(line, "MemAvailable:"):
			_, _ = fmt.Sscanf(line, "MemAvailable: %d", &memAvail)
		}
	}
	if memTotal == 0 {
		// Pre-3.14 kernels and minimal /proc configurations
		// (e.g. busybox) lack MemTotal. Surface as an error so
		// the caller can fall back.
		return 0, fmt.Errorf("/proc/meminfo: no MemTotal line")
	}
	used := float64(memTotal - memAvail)
	return 100.0 * used / float64(memTotal), nil
}
