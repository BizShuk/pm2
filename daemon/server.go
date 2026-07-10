package daemon

import (
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	_ "github.com/bizshuk/gosdk/log" // initialize gosdk slog default handler

	"github.com/bizshuk/pm2/daemon/network"
)

// Server is the PM2 daemon — a thin wrapper around ProcessManager that
// owns the Unix socket lifecycle and background goroutines (auto-save,
// auto-resurrect). All process management logic lives in ProcessManager;
// Server only coordinates the daemon's start-up and shut-down.
type Server struct {
	*ProcessManager
}

// NewServer returns a new Server initialised with a ProcessManager for
// the given home directory. The daemon is not listening yet — call
// Listen() to bind the socket and start accepting RPC requests.
func NewServer(homeDir string) *Server {
	return &Server{
		ProcessManager: NewProcessManager(homeDir),
	}
}

// Listen starts the Unix socket server. It wires up the metrics
// collector, auto-resurrect, and auto-save background goroutines,
// then delegates to network.Listen which blocks until the socket
// is closed or the daemon exits.
func (s *Server) Listen(socketPath string) error {
	s.StartMetricsCollector()

	go s.startAutoResurrect()
	go s.startAutoSave()

	return network.Listen(socketPath, s.ProcessManager)
}

func (s *Server) startAutoResurrect() {
	if err := s.Resurrect(); err != nil {
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file or directory") {
			slog.Info("auto-resurrect: no saved processes found (dump.json does not exist)")
		} else {
			slog.Error("auto-resurrect failed", "err", err)
		}
	} else {
		slog.Info("auto-resurrect completed successfully")
	}
}

func (s *Server) startAutoSave() {
	intervalStr := os.Getenv("PM2_AUTO_SAVE_INTERVAL")
	interval := 10 * time.Minute
	if intervalStr != "" {
		if d, err := time.ParseDuration(intervalStr); err == nil {
			interval = d
		} else {
			slog.Error("invalid PM2_AUTO_SAVE_INTERVAL", "interval", intervalStr, "err", err)
		}
	}

	slog.Info("auto-save enabled", "interval", interval, "firstRunIn", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		if err := s.Save(); err != nil {
			slog.Error("auto-save failed", "err", err)
		} else {
			slog.Info("auto-save: processes persisted successfully")
		}
	}
}