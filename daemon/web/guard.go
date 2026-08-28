package web

import (
	"sync"
	"time"
)

const (
	// rateWindow and rateBurst bound how often one workflow can be
	// triggered. This is not authentication and does not pretend to be —
	// it guards against an accident. An unauthenticated endpoint that
	// spawns processes turns a stuck retry loop, on any machine that can
	// reach it, into a fork bomb on this one.
	rateWindow = time.Minute
	rateBurst  = 10
)

type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	now    func() time.Time
	window time.Duration
	burst  int
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		hits:   make(map[string][]time.Time),
		now:    time.Now,
		window: rateWindow,
		burst:  rateBurst,
	}
}

// allow records an attempt and reports whether it is within budget.
func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := l.now().Add(-l.window)
	kept := l.hits[key][:0]
	for _, at := range l.hits[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) >= l.burst {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, l.now())
	return true
}
