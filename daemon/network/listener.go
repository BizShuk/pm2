package network

import (
	"fmt"
	"log/slog"
	"net"
	"os"
)

// Listen binds a Unix socket at socketPath and runs the accept loop
// until the listener returns an error (typically when the socket file
// is removed or the host shuts down). Each accepted connection is
// handed off to handler.Handle on a new goroutine.
//
// Any existing socket file at socketPath is removed first — the
// daemon is the sole owner of that path. If the listen call itself
// fails (e.g. permission denied, path in use), the original error
// is wrapped with %w so callers can errors.Is against it.
//
// The accept loop does NOT run background daemons (auto-resurrect,
// auto-save, metrics ticker). Those are owned by Manager / Server —
// the network layer's only job is to bind the socket and dispatch
// incoming connections.
func Listen(socketPath string, m Manager) error {
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		// ignore "file does not exist"; everything else (perm denied,
		// stale socket owned by another user) is fatal.
		return fmt.Errorf("remove stale socket: %w", err)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	slog.Info("daemon listening", "socketPath", socketPath)

	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go Handle(conn, m)
	}
}