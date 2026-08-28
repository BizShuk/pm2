package daemon

import (
	"net"
	"testing"
	"time"
)

// TestWebBindFailureDoesNotKillDaemon: the socket is the daemon's
// identity and it has already been claimed by the time the web server
// binds. Exiting because a UI port is busy would stop every managed
// process for a dashboard nobody may be looking at — and under launchd's
// KeepAlive it would retry forever against a port it can never own.
func TestWebBindFailureDoesNotKillDaemon(t *testing.T) {
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	defer blocker.Close()
	port := blocker.Addr().(*net.TCPAddr).Port

	srv := NewServer(t.TempDir())
	srv.WebHost, srv.WebPort = "127.0.0.1", port
	defer srv.KillAll()

	srv.startWeb()

	status := srv.Status()
	if status.WebError == "" {
		t.Error("a refused bind must be reported, not swallowed")
	}
	if status.WebAddr != "" {
		t.Errorf("no address should be reported after a failed bind, got %q", status.WebAddr)
	}

	// The daemon itself is unaffected: RPC still works.
	if srv.Status().PID == 0 {
		t.Error("daemon status broke after a failed web bind")
	}
}

func TestWebPortZeroDisablesTheServer(t *testing.T) {
	srv := NewServer(t.TempDir())
	srv.WebHost, srv.WebPort = "127.0.0.1", 0
	defer srv.KillAll()

	srv.startWeb()

	status := srv.Status()
	if status.WebAddr != "" || status.WebError != "" {
		t.Errorf("--web-port 0 should be silent, got addr=%q err=%q", status.WebAddr, status.WebError)
	}
}

func TestWebStatusReportsBoundAddress(t *testing.T) {
	srv := NewServer(t.TempDir())
	srv.WebHost, srv.WebPort = "127.0.0.1", freeTCPPort(t)
	defer srv.KillAll()

	srv.startWeb()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.Status().WebAddr != "" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("web address never reported: %+v", srv.Status())
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
