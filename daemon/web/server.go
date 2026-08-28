package web

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"
)

// Server owns the HTTP listener. Bind and Serve are separate so the
// daemon can report a bind failure and carry on rather than dying with
// it — see startWeb's rationale on the daemon side.
type Server struct {
	backend Backend
	history HistoryReader
	host    string
	port    int

	limiter  *rateLimiter
	listener net.Listener
	srv      *http.Server
}

// New returns a server for the given host and port. Passing DefaultHost
// publishes on every interface with no authentication; the package doc
// states what that means.
func New(b Backend, h HistoryReader, host string, port int) *Server {
	if host == "" {
		host = DefaultHost
	}
	if port == 0 {
		port = DefaultPort
	}
	return &Server{backend: b, history: h, host: host, port: port, limiter: newRateLimiter()}
}

// Bind claims the TCP port.
func (s *Server) Bind() error {
	ln, err := net.Listen("tcp", net.JoinHostPort(s.host, strconv.Itoa(s.port)))
	if err != nil {
		return fmt.Errorf("bind web server: %w", err)
	}
	s.listener = ln

	// Timeouts are not hygiene on a publicly reachable listener; without
	// them one half-open connection holds a goroutine and its buffers for
	// as long as the peer cares to keep it.
	s.srv = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 16,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}
	return nil
}

// Serve blocks until the server is closed. Bind must have succeeded.
func (s *Server) Serve() error {
	if s.listener == nil {
		return fmt.Errorf("web server not bound")
	}
	if err := s.srv.Serve(s.listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Addr is the bound address, or "" before a successful Bind.
func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// URL is the address in a form a person can paste into a browser.
func (s *Server) URL() string {
	if s.listener == nil {
		return ""
	}
	host := s.host
	if host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(s.port))
}

// Close stops accepting and drains briefly. The daemon's kill path exits
// the process outright, so this matters mainly to tests.
func (s *Server) Close() error {
	if s.srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.srv.Shutdown(ctx)
}
