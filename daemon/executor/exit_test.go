package executor

import (
	"errors"
	"os/exec"
	"testing"
)

func waitErrFor(t *testing.T, script string) error {
	t.Helper()
	cmd := exec.Command("/bin/bash", "-c", script)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %q: %v", script, err)
	}
	return cmd.Wait()
}

func TestExitInfoFromWait(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		code     int
		signal   string
		signaled bool
		known    bool
		success  bool
	}{
		{name: "clean exit", err: nil, code: 0, known: true, success: true},
		{name: "exit 3", err: waitErrFor(t, "exit 3"), code: 3, known: true},
		{
			name:     "killed by SIGTERM",
			err:      waitErrFor(t, "kill -TERM $$; sleep 5"),
			code:     143,
			signal:   "terminated",
			signaled: true,
			known:    true,
		},
		{name: "not an ExitError", err: errors.New("io: read/write on closed pipe"), code: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExitInfoFromWait(tt.err)
			if got.Code != tt.code {
				t.Errorf("Code: want %d, got %d", tt.code, got.Code)
			}
			if got.Signal != tt.signal {
				t.Errorf("Signal: want %q, got %q", tt.signal, got.Signal)
			}
			if got.Signaled != tt.signaled {
				t.Errorf("Signaled: want %v, got %v", tt.signaled, got.Signaled)
			}
			if got.Known != tt.known {
				t.Errorf("Known: want %v, got %v", tt.known, got.Known)
			}
			if got.Success() != tt.success {
				t.Errorf("Success: want %v, got %v", tt.success, got.Success())
			}
		})
	}
}

// TestExitInfoSignalIsNotReadAsExitCode pins the branch ordering: asking
// exec.ExitError.ExitCode() first would report -1 for every signalled
// process and lose the signal name entirely.
func TestExitInfoSignalIsNotReadAsExitCode(t *testing.T) {
	got := ExitInfoFromWait(waitErrFor(t, "kill -KILL $$; sleep 5"))
	if got.Code != 137 {
		t.Errorf("SIGKILL code: want 137, got %d", got.Code)
	}
	if got.Signal != "killed" {
		t.Errorf("SIGKILL signal: want %q, got %q", "killed", got.Signal)
	}
	if got.Success() {
		t.Error("a signalled process must never report success")
	}
}

// TestWatchReportsExitCode covers the whole path a caller actually uses:
// Watch is the only place cmd.Wait() is called for a managed process.
func TestWatchReportsExitCode(t *testing.T) {
	cmd := exec.Command("/bin/bash", "-c", "exit 7")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	done := make(chan ExitInfo, 1)
	NewExecutor(t.TempDir()).Watch(cmd, nil, nil, nil, nil, func(e ExitInfo) { done <- e })

	got := <-done
	if got.Code != 7 || !got.Known || got.Success() {
		t.Errorf("want {Code:7 Known:true Success:false}, got %+v (success=%v)", got, got.Success())
	}
}

// TestWatchWithoutChildIsNotSuccess guards the cron-task shape, where
// Watch is handed a nil cmd. "There was no child" must not read as
// "the child exited 0".
func TestWatchWithoutChildIsNotSuccess(t *testing.T) {
	done := make(chan ExitInfo, 1)
	NewExecutor(t.TempDir()).Watch(nil, nil, nil, nil, nil, func(e ExitInfo) { done <- e })

	got := <-done
	if got.Known || got.Success() {
		t.Errorf("nil cmd: want unknown outcome, got %+v", got)
	}
}
