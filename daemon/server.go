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

// ErrAlreadyRunning reports that another daemon already answers on the
// socket. Re-exported from network so CLI callers can react to the
// singleton guard without importing the daemon's network internals.
var ErrAlreadyRunning = network.ErrDaemonAlreadyRunning

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

// Listen starts the Unix socket server. It claims the socket, takes
// ownership of the daemon log, wires up the metrics collector,
// auto-resurrect, and auto-save background goroutines, then delegates
// to network.Serve which blocks until the socket is closed or the
// daemon exits.
//
// Claiming the socket comes first, and nothing else happens until it
// succeeds. A second daemon losing that race must leave the incumbent's
// state directory exactly as it found it — no rotated log, no
// resurrect replay, no auto-save tick.
func (s *Server) Listen(socketPath string) error {
	ln, err := network.Bind(socketPath)
	if err != nil {
		return err
	}
	defer ln.Close()

	defer installLogOrWarn(s.homeDir)()
	slog.Info("daemon listening", "socketPath", socketPath)

	s.StartMetricsCollector()
	s.pruneHistory()
	s.startWorkflows()

	go s.startAutoResurrect()
	go s.startAutoSave()

	return network.Serve(ln, s.ProcessManager)
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