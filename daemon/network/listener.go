package network

import (
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/bizshuk/pm2/model"
)

// probeTimeout bounds both the dial and the reply wait of the
// pre-bind liveness probe.
const probeTimeout = 2 * time.Second

// ErrDaemonAlreadyRunning is returned by Bind when a live daemon
// already answers on socketPath. Callers use errors.Is to tell this
// apart from a genuine bind failure.
var ErrDaemonAlreadyRunning = errors.New("daemon already running")

// Bind claims socketPath for this daemon and returns the bound
// listener.
//
// The daemon is a singleton per socket path. Before binding, Bind
// probes any existing socket with CmdPing: an answering daemon means
// this process must abort with ErrDaemonAlreadyRunning, and only a
// socket nobody answers on is removed as stale. Skipping the probe is
// what allowed two daemons to coexist — the second stole the path
// while the first kept running its own cron schedules, auto-restarts
// and dump.json writes against the same state directory.
//
// Binding is separate from serving so the caller can act on the
// outcome before committing to run: a refused start must not create
// log files, spawn background goroutines, or touch any state the
// incumbent daemon owns.
//
// If the listen call itself fails (e.g. permission denied, path in
// use), the original error is wrapped with %w so callers can
// errors.Is against it.
func Bind(socketPath string) (net.Listener, error) {
	if err := clearSocket(socketPath); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	return ln, nil
}

// Serve runs the accept loop until ln returns an error (typically
// when the socket file is removed or the host shuts down). Each
// accepted connection is handed off to Handle on a new goroutine.
//
// Serve does NOT run background daemons (auto-resurrect, auto-save,
// metrics ticker). Those are owned by Manager / Server — the network
// layer's only job is to dispatch incoming connections.
func Serve(ln net.Listener, m Manager) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go Handle(conn, m)
	}
}

// clearSocket makes socketPath free to bind, or reports why it is not.
//
// A path that answers CmdPing belongs to a live daemon and is left
// untouched. Anything else — no file, or a file no daemon answers on —
// is a stale leftover this process may remove. A dial that connects but
// never answers is also stale: the previous daemon's socket file
// outliving its process is the ordinary crash aftermath.
func clearSocket(socketPath string) error {
	if _, err := os.Stat(socketPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat socket: %w", err)
	}
	if daemonAnswers(socketPath) {
		return fmt.Errorf("%w on %s", ErrDaemonAlreadyRunning, socketPath)
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		// ignore "file does not exist"; everything else (perm denied,
		// stale socket owned by another user) is fatal.
		return fmt.Errorf("remove stale socket: %w", err)
	}
	return nil
}

// daemonAnswers reports whether a live daemon replies to CmdPing on
// socketPath.
//
// The probe carries its own deadline rather than going through
// model.SendRequest, whose read is unbounded: a daemon that is alive
// but wedged would accept the connection from the kernel backlog and
// never reply, leaving the starting daemon blocked forever on what is
// supposed to be a fast pre-flight check. An unanswered probe is
// reported as "no daemon" so a wedged predecessor can be replaced.
func daemonAnswers(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, probeTimeout)
	if err != nil {
		return false
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(probeTimeout)); err != nil {
		return false
	}
	if err := model.WriteJSON(conn, model.Request{Command: model.CmdPing}); err != nil {
		return false
	}
	var resp model.Response
	if err := model.ReadJSON(conn, &resp); err != nil {
		return false
	}
	return resp.OK
}