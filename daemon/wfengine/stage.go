package wfengine

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/bizshuk/pm2/daemon/executor"
	"github.com/bizshuk/pm2/logfile"
	"github.com/bizshuk/pm2/runhistory"
	"github.com/bizshuk/pm2/workflow"
)

// stageExec is everything needed to run one command once.
type stageExec struct {
	Script  string
	Args    []string
	Env     map[string]string
	BaseEnv []string
	CWD     string
	Timeout time.Duration
}

// execStage spawns the command, waits for it, and fills in the result.
//
// It deliberately does not go through executor.Start: that path opens
// task log files keyed on a process name, registers nothing, and hands
// the exit to onProcessExit's auto-restart loop. A stage that exits 1 is
// finished, not crashed.
func (e *Engine) execStage(ctx context.Context, se stageExec, cfg workflow.Config, ar *activeRun, idx int, result *workflow.StageRun) {
	logName := runhistory.StageLogName(cfg.Key(), ar.runID(), result.Name)
	result.LogName = logName
	result.Command = commandLine(se)

	if err := os.MkdirAll(runhistory.WorkflowLogsDir(e.homeDir), 0o755); err != nil {
		result.Status = runhistory.StatusFailed
		result.Error = fmt.Sprintf("create workflow log directory: %v", err)
		return
	}
	out, err := logfile.Open(e.runLogPath(logName))
	if err != nil {
		result.Status = runhistory.StatusFailed
		result.Error = fmt.Sprintf("open stage log: %v", err)
		return
	}
	defer out.Close()

	cmd := executor.BuildCommand(se.Script, se.Args, se.BaseEnv, se.Env, se.CWD)
	// stdout and stderr share one file. A stage is one command with one
	// story, and interleaving them is how a human reads it; the split
	// pm2 keeps for a supervised task exists because two long-lived
	// streams are worth separating, which a one-shot stage is not.
	cmd.Stdout, cmd.Stderr = out, out

	if err := cmd.Start(); err != nil {
		result.Status = runhistory.StatusFailed
		result.Error = err.Error()
		return
	}
	result.PID = cmd.Process.Pid
	// Publish the stage as soon as it has a PID. The journal only holds
	// finished runs, so LiveRuns is the only place an in-flight stage is
	// visible — and a run whose current stage shows PID 0 is useless to
	// anyone trying to find the process.
	ar.publishStage(idx, *result)

	waited := make(chan executor.ExitInfo, 1)
	done := make(chan struct{})
	go func() {
		waited <- executor.ExitInfoFromWait(cmd.Wait())
		close(done)
	}()

	// A timeout is just a deadline-shaped cancellation, so both take the
	// same exit: executor.Stop, which signals the whole process group
	// (BuildCommand sets Setpgid precisely so it can) and escalates to
	// SIGKILL. Re-implementing that here would leave grandchildren alive.
	var timeout <-chan time.Time
	if se.Timeout > 0 {
		timer := time.NewTimer(se.Timeout)
		defer timer.Stop()
		timeout = timer.C
	}

	select {
	case exit := <-waited:
		fillExit(result, exit)
	case <-timeout:
		e.exec.Stop(cmd, done, nil, nil)
		fillExit(result, <-waited)
		result.Status = runhistory.StatusFailed
		result.Error = fmt.Sprintf("stage exceeded its %s timeout", se.Timeout)
	case <-ctx.Done():
		e.exec.Stop(cmd, done, nil, nil)
		fillExit(result, <-waited)
		result.Status = runhistory.StatusCancelled
		result.Error = "run cancelled"
	}
}

// fillExit records the outcome. Success is exit code 0 and nothing else:
// a signalled stage failed even though 128+N is an ordinary integer.
func fillExit(result *workflow.StageRun, exit executor.ExitInfo) {
	result.ExitCode = exit.Code
	result.ExitKnown = exit.Known
	result.Signal = exit.Signal
	if exit.Success() {
		result.Status = runhistory.StatusSuccess
		return
	}
	result.Status = runhistory.StatusFailed
	if exit.Err != nil {
		result.Error = exit.Err.Error()
	}
}

// commandLine is the human-readable form recorded on the stage, not
// something re-parsed for execution.
func commandLine(se stageExec) string {
	line := se.Script
	for _, a := range se.Args {
		line += " " + a
	}
	return line
}
