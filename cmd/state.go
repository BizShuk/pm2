package cmd

import (
	"fmt"
	"os"
	"path/filepath"
)

var pm2Home string

const daemonStopMarkerFile = "daemon.stopped"

func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot determine home dir:", err)
		os.Exit(1)
	}
	pm2Home = filepath.Join(home, ".pm2")
	_ = os.MkdirAll(pm2Home, 0755)
}

func socketPath() string {
	return filepath.Join(pm2Home, "pm2.sock")
}

// PM2Home returns the shared state directory used by CLI command packages.
func PM2Home() string {
	return pm2Home
}

// SocketPath returns the daemon socket used by CLI command packages.
func SocketPath() string {
	return socketPath()
}

// DaemonStopMarkerPath returns the durable marker that disables silent
// daemon auto-spawn after an explicit `pm2 daemon stop`.
func DaemonStopMarkerPath() string {
	return filepath.Join(pm2Home, daemonStopMarkerFile)
}
