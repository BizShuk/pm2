package daemon

import (
	"log/slog"
	"sync"

	"github.com/bizshuk/pm2/daemon/web"
)

// webState records where the HTTP dashboard is listening, or why it is
// not, so Status can report it and `pm2 daemon status` can print it.
type webState struct {
	mu   sync.RWMutex
	addr string
	err  string
}

func (s *webState) set(addr, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addr, s.err = addr, errMsg
}

func (s *webState) read() (string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.addr, s.err
}

// startWeb binds and serves the HTTP dashboard.
//
// A bind failure degrades the daemon; it never fails it. Three reasons,
// all from this repo:
//
//   - The Unix socket is the daemon's identity and it has already been
//     claimed. Exiting because a UI port is busy would stop every managed
//     process for a dashboard nobody may be looking at.
//   - launchd's KeepAlive={SuccessfulExit:false} would retry forever
//     against a port it can never own — the same failure mode CLAUDE.md
//     documents for the singleton guard's exit code.
//   - Precedent: a failed file watcher does not fail a launch, and a
//     failed log install does not stop the daemon.
//
// But a refusal is a message, not silence: the failure is logged and
// surfaced through DaemonInfo.WebError.
func (s *Server) startWeb() {
	if s.WebPort < 0 {
		return
	}
	if s.WebPort == 0 {
		slog.Info("web server disabled")
		return
	}

	srv := web.New(webBackend{pm: s.ProcessManager}, s.history, s.WebHost, s.WebPort)
	if err := srv.Bind(); err != nil {
		slog.Error("web server unavailable; the daemon continues without it",
			"host", s.WebHost, "port", s.WebPort, "err", err)
		s.web.set("", err.Error())
		return
	}

	s.web.set(srv.Addr(), "")

	// The exposure has to be visible to whoever reads the daemon's log,
	// not only to whoever reads the source. This endpoint accepts a
	// webhook that runs shell commands.
	slog.Warn("web server listening WITHOUT AUTHENTICATION",
		"addr", srv.Addr(), "url", srv.URL(),
		"note", "anyone who can reach this address can trigger a workflow, which runs shell commands on this machine")

	go func() {
		if err := srv.Serve(); err != nil {
			slog.Error("web server stopped", "err", err)
			s.web.set("", err.Error())
		}
	}()
}
