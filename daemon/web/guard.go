package web

import (
	"net/http"
	"net/url"
	"strings"
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

// The guard below exists because "reachable only from the LAN" is not a
// boundary a browser respects.
//
// A page on evil.com, opened by anyone on this network, can POST to this
// server from their own browser. The attacker cannot read the response,
// but the workflow runs anyway — and a workflow stage runs a shell
// command. The bind address does nothing about that, which is why this
// check is load-bearing rather than decorative.
//
// It is not authentication and does not pretend to be. It is transparent
// to every real client: curl, CI runners, and scripts send no Origin at
// all, and the dashboard sends one that matches its own Host.

// sameOrigin reports whether a request may proceed.
//
// A request with no Origin is accepted — that is every non-browser
// client. A request carrying an Origin must have it match the Host the
// request was actually addressed to, which works for a LAN name, a LAN
// IP, or localhost without any of them having to be enumerated. A page
// on another site fails it by construction.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// guard applies the check to every route, not just the webhook: the read
// endpoints expose the task table, its configuration, and the run
// history, which a page on another origin has no business pulling either.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			writeErr(w, http.StatusForbidden, "cross-origin requests are not accepted")
			return
		}
		next.ServeHTTP(w, r)
	})
}
