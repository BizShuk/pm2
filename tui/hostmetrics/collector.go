// Package hostmetrics abstracts whole-system CPU% and Memory%
// collection behind a single interface so the TUI can swap in
// implementations per OS (or a stub during tests).
//
// The interface is intentionally tiny: one Collect() method that
// returns the latest reading or an error. Callers (today: the
// Bubble Tea model) decide what to do on error — the canonical
// fallback is 5.2 / 64.1, which mirrors the cosmetic values the
// legacy macOS `top` parser used when its own parse failed.
//
// This package must not import tui/views, tui/theme, or cmd/.
// It also must not render anything: callers are expected to feed
// the (cpu, mem) pair into whatever formatting they own. Keeping
// the package pure-numeric is the whole point of the split.
package hostmetrics

import "runtime"

// HostMetricsCollector returns whole-system CPU% (0-100) and
// memory% (0-100) for the local host.
type HostMetricsCollector interface {
	// Collect returns the latest sample. err is non-nil if the
	// implementation could not read the underlying OS source
	// (e.g. /proc/stat unreadable inside a sandboxed container);
	// the returned cpu/mem values are zero in that case.
	Collect() (cpu float64, mem float64, err error)
}

// NewCollector returns the platform-appropriate collector. The
// switch is evaluated at construction time; callers that need a
// stub for tests should construct the desired implementation
// directly (e.g. fallbackCollector{}) rather than going through
// this factory.
func NewCollector() HostMetricsCollector {
	switch runtime.GOOS {
	case "darwin":
		return newDarwinCollector()
	case "linux":
		return newLinuxCollector()
	default:
		return newFallbackCollector()
	}
}
