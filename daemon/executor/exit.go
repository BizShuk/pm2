package executor

import (
	"errors"
	"os/exec"
	"syscall"
)

// ExitInfo is what cmd.Wait() actually reported.
//
// Watch used to hand its caller a bare error, so nothing in pm2 could
// tell "exited 3" from "killed by SIGTERM" from "never started". A
// workflow stage needs that distinction to decide whether the next
// stage runs, and the run journal needs it to record an outcome a
// human can act on.
//
// Code is the shell convention: 0 on clean exit, the child's own code
// on a normal non-zero exit, and 128+signal when the child was
// terminated by a signal. It is -1 only when there is genuinely no
// code to report (the child never started, or Wait failed for a reason
// unrelated to the child's exit status) — which is why Known exists:
// a caller must not read -1 as "exited with 255".
type ExitInfo struct {
	Err      error
	Code     int
	Signal   string
	Signaled bool
	Known    bool
}

// Success reports whether the process finished cleanly. Nothing else
// counts: a signalled process is a failure even though its 128+N code
// is a perfectly ordinary integer.
func (e ExitInfo) Success() bool { return e.Known && e.Code == 0 }

// ExitInfoFromWait converts a cmd.Wait() error into an ExitInfo.
//
// The signal branch must come first: exec.ExitError.ExitCode() returns
// -1 for a process terminated by a signal, so asking it first would
// throw away the only interesting fact about a killed process.
func ExitInfoFromWait(err error) ExitInfo {
	if err == nil {
		return ExitInfo{Code: 0, Known: true}
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			sig := ws.Signal()
			return ExitInfo{
				Err:      err,
				Code:     128 + int(sig),
				Signal:   sig.String(),
				Signaled: true,
				Known:    true,
			}
		}
		return ExitInfo{Err: err, Code: exitErr.ExitCode(), Known: true}
	}

	return ExitInfo{Err: err, Code: -1}
}
