package network

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/workflow"
)

// pingManager is the smallest Manager that answers CmdPing, which is
// all the singleton guard probes for.
type pingManager struct{}

func (pingManager) StartApp(_ *model.AppStartReq) ([]process.ProcessInfo, error) {
	return nil, nil
}
func (pingManager) StopByName(_ string) error      { return nil }
func (pingManager) RestartByName(_ string) error   { return nil }
func (pingManager) PauseByName(_ string) error     { return nil }
func (pingManager) ResumeByName(_ string) error    { return nil }
func (pingManager) DeleteByName(_ string) error    { return nil }
func (pingManager) ListAll() []process.ProcessInfo { return nil }
func (pingManager) Save() error                    { return nil }
func (pingManager) Resurrect() error               { return nil }
func (pingManager) KillAll()                       {}
func (pingManager) Ping()                          {}
func (pingManager) Status() process.DaemonInfo     { return process.DaemonInfo{} }

func (pingManager) RegisterWorkflows(_ []workflow.Config) ([]string, []string, error) {
	return nil, nil, nil
}
func (pingManager) ListWorkflows() []workflow.Status { return nil }
func (pingManager) RunWorkflow(_, _ string, _ bool) (workflow.Run, error) {
	return workflow.Run{}, nil
}
func (pingManager) DeleteWorkflow(_ string) error  { return nil }
func (pingManager) StopWorkflowRun(_ string) error { return nil }

// TestListenRefusesSecondDaemon is the regression guard for the
// split-brain bug: two daemons ran against one ~/.pm2 because Listen
// removed and rebound the socket unconditionally. The first daemon
// kept its cron schedules, auto-restarts and dump.json writes while
// the CLI only ever saw the second.
func TestListenRefusesSecondDaemon(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "pm2.sock")

	serve(t, sock)

	_, err := Bind(sock)
	if !errors.Is(err, ErrDaemonAlreadyRunning) {
		t.Fatalf("second Bind: got %v, want ErrDaemonAlreadyRunning", err)
	}

	// The live daemon must still own a working socket — a refused
	// start that damages the incumbent is no better than the bug.
	if !daemonAnswers(sock) {
		t.Fatal("incumbent daemon stopped answering after the refused start")
	}
}

// TestListenReplacesStaleSocket covers the ordinary crash aftermath:
// the socket file outlives its daemon, and nobody answers on it.
func TestListenReplacesStaleSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "pm2.sock")

	// A socket file with no listener behind it.
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("seed socket: %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("close seed listener: %v", err)
	}
	if _, err := os.Stat(sock); err != nil {
		t.Skipf("platform removes the socket file on close: %v", err)
	}

	serve(t, sock)

	if !daemonAnswers(sock) {
		t.Fatal("daemon did not take over the stale socket path")
	}
}

// TestClearSocketOnFreePath keeps the first-boot path simple: no file,
// no probe, no error.
func TestClearSocketOnFreePath(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "pm2.sock")
	if err := clearSocket(sock); err != nil {
		t.Fatalf("clearSocket on free path: %v", err)
	}
}

// serve binds sock, runs the accept loop for the duration of the test,
// and returns once the daemon answers.
func serve(t *testing.T, sock string) {
	t.Helper()

	ln, err := Bind(sock)
	if err != nil {
		t.Fatalf("bind %s: %v", sock, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() { _ = Serve(ln, pingManager{}) }()

	for range 50 {
		if daemonAnswers(sock) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("daemon never answered on socket")
}
