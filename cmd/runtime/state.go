// Package runtime owns shared CLI state and daemon RPC infrastructure.
package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const daemonStopMarkerFile = "daemon.stopped"

// pm2Home resolves ~/.pm2 on first use, not at package init.
//
// Resolving it in an init() made every pm2 command — including the ones
// that never touch the state directory — die on any process with no
// $HOME, and take the whole binary down before main ran. A LaunchDaemon
// in the system domain is exactly that process: launchd passes it no
// HOME, so `pm2 gpu agent` exited 1 six times in a row without ever
// reaching its own code. Creating ~/.pm2 as a side effect of starting
// any command was the same mistake in a quieter form.
//
// A command that genuinely needs the directory still fails loudly, just
// at the point of use, where the error names something the caller asked
// for.
var homeDir = sync.OnceValue(func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot determine home dir:", err)
		os.Exit(1)
	}
	return home
})

var pm2Home = sync.OnceValue(func() string {
	dir := filepath.Join(homeDir(), ".pm2")
	_ = os.MkdirAll(dir, 0o755)
	return dir
})

func socketPath() string {
	return filepath.Join(pm2Home(), "pm2.sock")
}

// PM2Home returns the shared state directory used by CLI command packages.
func PM2Home() string {
	return pm2Home()
}

// SocketPath returns the daemon socket used by CLI command packages.
func SocketPath() string {
	return socketPath()
}

// ConfigRoot returns ~/.config, the fixed root gosdk's config.Default gives
// every application and therefore where managed task logs live
// (~/.config/<app>/logs). It is never created here — an application owns its
// own directory, and a browser that reads them must not invent any.
func ConfigRoot() string {
	return filepath.Join(homeDir(), ".config")
}

// DaemonStopMarkerPath returns the durable marker that disables silent
// daemon auto-spawn after an explicit `pm2 daemon stop`.
func DaemonStopMarkerPath() string {
	return filepath.Join(pm2Home(), daemonStopMarkerFile)
}
