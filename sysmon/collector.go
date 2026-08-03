package sysmon

import (
	"errors"
	"os"
	"runtime"
	"sync"
)

// ErrUnsupportedPlatform is returned by the fallback sampler on an OS
// with no implementation. Callers should render "—" rather than treat
// it as a hard failure.
var ErrUnsupportedPlatform = errors.New("sysmon: no system sampler for this platform")

// sampler is the per-platform half of the collector: everything that has
// to know whether it is reading `iostat` or `/proc`. It returns finished
// values, including rates, because the counters each platform exposes
// differ too much for a shared conversion step to be honest.
//
// Implementations hold rate state, so a sampler is used from exactly one
// Collector and its own mutex serialises concurrent samples.
type sampler interface {
	sample() (System, error)
	host() (Host, error)
}

// Collector is the entry point for every system reading. Construct one
// per process (New is cheap but the returned value carries the counter
// baselines that make rates meaningful) and reuse it; all methods are
// safe for concurrent use.
type Collector struct {
	mu      sync.Mutex
	sampler sampler
}

// New returns a Collector bound to the sampler for the running OS.
func New() *Collector {
	return &Collector{sampler: newSampler(runtime.GOOS)}
}

// newSampler is split out from New so tests can pin a platform without
// running on it.
func newSampler(goos string) sampler {
	switch goos {
	case "darwin":
		return newDarwinSampler()
	case "linux":
		return newLinuxSampler()
	default:
		return newFallbackSampler()
	}
}

// Sample returns one whole-machine reading. On macOS the call blocks for
// roughly one second because that is the shortest honest CPU window
// `iostat` will report; call it from a goroutine, never from a render
// path.
func (c *Collector) Sample() (System, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sampler.sample()
}

// Host returns the static machine identity plus uptime.
func (c *Collector) Host() (Host, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	host, err := c.sampler.host()
	if host.Hostname == "" {
		if name, nameErr := os.Hostname(); nameErr == nil {
			host.Hostname = name
		}
	}
	host.OS = runtime.GOOS
	host.Arch = runtime.GOARCH
	return host, err
}

// Processes returns the full OS process table, newest reading each call.
// It is exported so a caller that only wants the process view (the
// dashboard's "all processes" mode) does not pay for a System sample.
func (c *Collector) Processes() ([]Proc, error) {
	return readProcessTable(runtime.GOOS)
}

// ListeningPorts returns every listening socket the current user can see,
// keyed by owning PID.
func (c *Collector) ListeningPorts() (map[int][]Port, error) {
	return readListeningPorts(runtime.GOOS)
}
