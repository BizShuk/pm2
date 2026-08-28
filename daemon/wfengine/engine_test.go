package wfengine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/runhistory"
	"github.com/bizshuk/pm2/workflow"
)

// fakeTasks stands in for the daemon's registry.
type fakeTasks struct {
	byRef map[string]process.AppConfig
}

func (f fakeTasks) LookupTask(ref string) (process.AppConfig, error) {
	if cfg, ok := f.byRef[ref]; ok {
		return cfg, nil
	}
	return process.AppConfig{}, fmt.Errorf("task %q not found", ref)
}

func newTestEngine(t *testing.T, tasks TaskLookup) (*Engine, string) {
	t.Helper()
	home := t.TempDir()
	if tasks == nil {
		tasks = fakeTasks{}
	}
	e := New(home, tasks, runhistory.NewStore(home))
	t.Cleanup(e.Close)
	return e, home
}

func script(name string, stages ...workflow.Stage) workflow.Config {
	cfg := workflow.Config{Category: "ci", Name: name, Stages: stages}
	cfg.Normalize("")
	return cfg
}

func stage(name, sh string) workflow.Stage {
	return workflow.Stage{Name: name, Script: sh}
}

func mustRegister(t *testing.T, e *Engine, cfgs ...workflow.Config) {
	t.Helper()
	if _, _, err := e.Register(cfgs); err != nil {
		t.Fatalf("Register: %v", err)
	}
}

func TestLinearRunSucceeds(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	mustRegister(t, e, script("ok",
		stage("one", "true"), stage("two", "true"), stage("three", "true")))

	run, err := e.Run("ci:ok", runhistory.TriggerManual, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if run.Status != runhistory.StatusSuccess {
		t.Errorf("status: want %q, got %q (%s)", runhistory.StatusSuccess, run.Status, run.Error)
	}
	for i, st := range run.Stages {
		if st.Status != runhistory.StatusSuccess || st.ExitCode != 0 {
			t.Errorf("stage %d: want success/0, got %s/%d", i, st.Status, st.ExitCode)
		}
	}
}

// TestRunStopsAtFirstFailure also pins that the remaining stages are
// *recorded* as skipped, not omitted: a reader must see the whole
// declared sequence to know where it stopped.
func TestRunStopsAtFirstFailure(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	marker := filepath.Join(t.TempDir(), "third-ran")
	mustRegister(t, e, script("halts",
		stage("one", "true"),
		stage("two", "exit 3"),
		stage("three", "touch "+marker)))

	run, err := e.Run("ci:halts", runhistory.TriggerManual, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if run.Status != runhistory.StatusFailed {
		t.Errorf("run status: want failed, got %q", run.Status)
	}
	if run.Stages[1].ExitCode != 3 || run.Stages[1].Status != runhistory.StatusFailed {
		t.Errorf("stage 2: want failed/3, got %s/%d", run.Stages[1].Status, run.Stages[1].ExitCode)
	}
	if run.Stages[2].Status != runhistory.StatusSkipped {
		t.Errorf("stage 3 must be recorded as skipped, got %q", run.Stages[2].Status)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("stage 3 must not have run")
	}
}

// TestFailingStageRunsExactlyOnce is the no-auto-restart invariant. A
// task with MaxRestarts 15 would be resurrected fifteen times on the
// supervised path; a stage that exits 1 is finished.
func TestFailingStageRunsExactlyOnce(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	counter := filepath.Join(t.TempDir(), "count")
	mustRegister(t, e, script("once",
		stage("fail", fmt.Sprintf("echo x >> %s; exit 1", counter))))

	if _, err := e.Run("ci:once", runhistory.TriggerManual, true); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Well past any restart delay a supervised process would use.
	time.Sleep(300 * time.Millisecond)

	data, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	if got := strings.Count(string(data), "x"); got != 1 {
		t.Errorf("stage must run exactly once, ran %d times", got)
	}
}

// TestTaskStageIgnoresSupervisionFields: instances, cron, and
// max_restarts describe how a task is supervised, which has no meaning
// for a single execution.
func TestTaskStageIgnoresSupervisionFields(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "count")
	tasks := fakeTasks{byRef: map[string]process.AppConfig{
		"worker": {
			Name: "worker", Script: fmt.Sprintf("echo x >> %s; exit 1", counter),
			Instances: 3, Cron: "@every 1s", CronRestart: "@every 1s",
			MaxRestarts: 15, Watch: true, Paused: true,
		},
	}}
	e, _ := newTestEngine(t, tasks)
	mustRegister(t, e, script("viatask", workflow.Stage{Name: "run", Task: "worker"}))

	run, err := e.Run("ci:viatask", runhistory.TriggerManual, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if run.Stages[0].ExitCode != 1 {
		t.Errorf("exit code: want 1, got %d", run.Stages[0].ExitCode)
	}

	time.Sleep(300 * time.Millisecond)
	data, _ := os.ReadFile(counter)
	if got := strings.Count(string(data), "x"); got != 1 {
		t.Errorf("a paused, cron-scheduled, 3-instance task must still run once: ran %d", got)
	}
	if n := e.scheduler.EntryCount(); n != 0 {
		t.Errorf("a task stage must not arm a schedule, got %d entries", n)
	}
}

func TestMissingTaskFailsTheStage(t *testing.T) {
	e, _ := newTestEngine(t, fakeTasks{})
	mustRegister(t, e, script("missing", workflow.Stage{Name: "run", Task: "nosuch"}))

	run, err := e.Run("ci:missing", runhistory.TriggerManual, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if run.Status != runhistory.StatusFailed || !strings.Contains(run.Stages[0].Error, "nosuch") {
		t.Errorf("want a failure naming the task, got %q / %q", run.Status, run.Stages[0].Error)
	}
}

// TestSingleFlightRejectsExplicitTrigger: somebody is waiting for an
// answer, so silence would be a lie.
func TestSingleFlightRejectsExplicitTrigger(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	mustRegister(t, e, script("slow", stage("wait", "sleep 5")))

	first, err := e.Run("ci:slow", runhistory.TriggerManual, false)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	defer e.StopRun(first.ID)

	_, err = e.Run("ci:slow", runhistory.TriggerWebhook, false)
	if !errors.Is(err, ErrRunInProgress) {
		t.Fatalf("want ErrRunInProgress, got %v", err)
	}
	if !strings.Contains(err.Error(), first.ID) {
		t.Errorf("error should name the run in flight (%s), got %q", first.ID, err)
	}
}

// TestCronFireSkipIsRecorded: a cron fire that lands on a running
// workflow is normal — the workflow runs late, it is not truncated —
// but the fire still has to leave a trace.
func TestCronFireSkipIsRecorded(t *testing.T) {
	e, home := newTestEngine(t, nil)
	mustRegister(t, e, script("slow", stage("wait", "sleep 5")))

	first, err := e.Run("ci:slow", runhistory.TriggerManual, false)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	e.cronFire("ci:slow")()

	recs, err := runhistory.NewStore(home).RecentWorkflows(runhistory.Query{})
	if err != nil {
		t.Fatalf("RecentWorkflows: %v", err)
	}
	if len(recs) != 1 || recs[0].Status != runhistory.StatusSkipped {
		t.Fatalf("want one skipped record, got %#v", recs)
	}
	if recs[0].Trigger != runhistory.TriggerCron {
		t.Errorf("trigger: want cron, got %q", recs[0].Trigger)
	}

	e.StopRun(first.ID)
}

// TestRuntimeCycleGuard constructs the definition map directly so the
// static check never sees the cycle — which is exactly the position the
// engine is in when a workflow is registered from a second file.
func TestRuntimeCycleGuard(t *testing.T) {
	e, _ := newTestEngine(t, nil)

	a := script("a", workflow.Stage{Name: "call", Workflow: "ci:b"})
	b := script("b", workflow.Stage{Name: "call", Workflow: "ci:a"})
	e.mu.Lock()
	e.defs["ci:a"], e.defs["ci:b"] = a, b
	e.mu.Unlock()

	run, err := e.Run("ci:a", runhistory.TriggerManual, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if run.Status != runhistory.StatusFailed {
		t.Fatalf("want the run to fail, got %q", run.Status)
	}
	if !strings.Contains(run.Error, "workflow cycle") {
		t.Errorf("want a cycle error, got %q", run.Error)
	}

	e.mu.RLock()
	left := len(e.inflight)
	e.mu.RUnlock()
	if left != 0 {
		t.Errorf("a rejected cycle must not wedge the single-flight slot, %d left", left)
	}
}

func TestNestingDepthCap(t *testing.T) {
	e, _ := newTestEngine(t, nil)

	// A chain of 10 distinct workflows: w1 -> w2 -> ... -> w10.
	e.mu.Lock()
	for i := 1; i <= 10; i++ {
		name := fmt.Sprintf("w%d", i)
		var stages []workflow.Stage
		if i < 10 {
			stages = []workflow.Stage{{Name: "next", Workflow: fmt.Sprintf("ci:w%d", i+1)}}
		} else {
			stages = []workflow.Stage{stage("leaf", "true")}
		}
		cfg := script(name, stages...)
		e.defs[cfg.Key()] = cfg
	}
	e.mu.Unlock()

	run, err := e.Run("ci:w1", runhistory.TriggerManual, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if run.Status != runhistory.StatusFailed || !strings.Contains(run.Error, "nesting depth") {
		t.Errorf("want a depth-limit failure, got %q / %q", run.Status, run.Error)
	}
}

func TestNestedRunLinksToParent(t *testing.T) {
	e, home := newTestEngine(t, nil)
	mustRegister(t, e,
		script("parent", workflow.Stage{Name: "call", Workflow: "ci:child"}),
		script("child", stage("work", "true")))

	run, err := e.Run("ci:parent", runhistory.TriggerManual, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if run.Status != runhistory.StatusSuccess {
		t.Fatalf("want success, got %q (%s)", run.Status, run.Error)
	}

	childID := run.Stages[0].ChildRunID
	if childID == "" {
		t.Fatal("parent stage must link to the child run")
	}

	child, ok, err := runhistory.NewStore(home).WorkflowRun(childID)
	if err != nil || !ok {
		t.Fatalf("child run not journaled: ok=%v err=%v", ok, err)
	}
	if child.ParentRunID != run.ID {
		t.Errorf("child parent: want %s, got %s", run.ID, child.ParentRunID)
	}
	if child.Depth != 1 {
		t.Errorf("child depth: want 1, got %d", child.Depth)
	}
	if child.Trigger != runhistory.TriggerNested {
		t.Errorf("child trigger: want nested, got %q", child.Trigger)
	}
}

func TestStopRunCancelsAndFreesTheSlot(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	mustRegister(t, e, script("long", stage("wait", "sleep 60")))

	run, err := e.Run("ci:long", runhistory.TriggerManual, false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	waitForPID(t, e)

	start := time.Now()
	if err := e.StopRun(run.ID); err != nil {
		t.Fatalf("StopRun: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Errorf("stop took %s; executor.Stop escalates within 5s", elapsed)
	}

	// The slot must be free, or this workflow is disabled forever.
	if _, err := e.Run("ci:long", runhistory.TriggerManual, false); err != nil {
		t.Fatalf("workflow must be runnable again after a stop: %v", err)
	}
	for _, r := range e.LiveRuns() {
		e.StopRun(r.ID)
	}
}

// TestStageTimeoutKillsTheProcessGroup is what Setpgid plus
// executor.Stop buy: a grandchild must not outlive the stage.
func TestStageTimeoutKillsTheProcessGroup(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")

	cfg := workflow.Config{Category: "ci", Name: "slow", Stages: []workflow.Stage{{
		Name:    "hang",
		Script:  fmt.Sprintf("sleep 60 & echo $! > %s; sleep 60", pidFile),
		Timeout: "300ms",
	}}}
	cfg.Normalize("")
	mustRegister(t, e, cfg)

	run, err := e.Run("ci:slow", runhistory.TriggerManual, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if run.Status != runhistory.StatusFailed {
		t.Errorf("a timed-out run must fail, got %q", run.Status)
	}
	if !strings.Contains(run.Stages[0].Error, "timeout") {
		t.Errorf("want a timeout error, got %q", run.Stages[0].Error)
	}
	if run.Stages[0].Signal == "" {
		t.Errorf("a killed stage must record its signal, got %+v", run.Stages[0])
	}

	pid := readPID(t, pidFile)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("grandchild %d survived the stage; the process group was not signalled", pid)
}

// TestBaseEnvReachesTheStage: without it a stage script runs with the
// daemon's minimal PATH rather than the user's.
func TestBaseEnvReachesTheStage(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	outFile := filepath.Join(t.TempDir(), "marker")

	cfg := workflow.Config{
		Category: "ci", Name: "env",
		BaseEnv: []string{"PM2_WF_MARKER=from-base-env"},
		Env:     map[string]string{"WORKFLOW_LEVEL": "yes"},
		Stages: []workflow.Stage{{
			Name:   "echo",
			Script: fmt.Sprintf(`echo "$PM2_WF_MARKER/$WORKFLOW_LEVEL/$STAGE_LEVEL" > %s`, outFile),
			Env:    map[string]string{"STAGE_LEVEL": "yes"},
		}},
	}
	cfg.Normalize("")
	mustRegister(t, e, cfg)

	if _, err := e.Run("ci:env", runhistory.TriggerManual, true); err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "from-base-env/yes/yes" {
		t.Errorf("env layering: want %q, got %q", "from-base-env/yes/yes", got)
	}
}

func TestStageOutputIsWrittenToItsLog(t *testing.T) {
	e, home := newTestEngine(t, nil)
	mustRegister(t, e, script("logged", stage("talk", "echo hello-stdout; echo hello-stderr 1>&2")))

	run, err := e.Run("ci:logged", runhistory.TriggerManual, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	path := runhistory.NewStore(home).StageLogPath("ci:logged", run.ID, "talk")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stage log %s: %v", path, err)
	}
	for _, want := range []string{"hello-stdout", "hello-stderr"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("stage log missing %q; got %q", want, data)
		}
	}
}

func TestRegisterRejectsCyclesWithoutChangingAnything(t *testing.T) {
	e, home := newTestEngine(t, nil)
	mustRegister(t, e, script("safe", stage("work", "true")))

	before, err := os.ReadFile(workflow.DumpPath(home))
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}

	_, _, err = e.Register([]workflow.Config{
		script("a", workflow.Stage{Name: "c", Workflow: "ci:b"}),
		script("b", workflow.Stage{Name: "c", Workflow: "ci:a"}),
	})
	if err == nil || !strings.Contains(err.Error(), "workflow cycle") {
		t.Fatalf("want a cycle rejection, got %v", err)
	}

	after, err := os.ReadFile(workflow.DumpPath(home))
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	if string(before) != string(after) {
		t.Error("a rejected batch must leave the dump byte-identical")
	}
	if len(e.List()) != 1 {
		t.Errorf("a rejected batch must register nothing, got %d workflows", len(e.List()))
	}
}

func TestLoadRestoresDefinitionsAndArmsCron(t *testing.T) {
	e, home := newTestEngine(t, nil)
	cfg := script("scheduled", stage("work", "true"))
	cfg.Cron = "@every 1h"
	mustRegister(t, e, cfg)

	if n := e.scheduler.EntryCount(); n != 1 {
		t.Fatalf("want 1 armed schedule, got %d", n)
	}

	// A fresh engine over the same home, as a daemon restart would build.
	restarted := New(home, fakeTasks{}, runhistory.NewStore(home))
	defer restarted.Close()
	if err := restarted.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := restarted.List(); len(got) != 1 || got[0].Key() != "ci:scheduled" {
		t.Fatalf("definitions did not survive a restart: %#v", got)
	}
	if n := restarted.scheduler.EntryCount(); n != 1 {
		t.Errorf("cron must be re-armed after a restart, got %d entries", n)
	}
}

func TestLoadWithoutDumpIsNotAnError(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	if err := e.Load(); err != nil {
		t.Errorf("a daemon that never applied a workflow has no dump: %v", err)
	}
}

func TestDeleteRemovesDefinitionAndSchedule(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	cfg := script("temp", stage("work", "true"))
	cfg.Cron = "@every 1h"
	mustRegister(t, e, cfg)

	if err := e.Delete("ci:temp"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(e.List()) != 0 {
		t.Error("definition should be gone")
	}
	if n := e.scheduler.EntryCount(); n != 0 {
		t.Errorf("schedule should be disarmed, got %d entries", n)
	}
	if err := e.Delete("ci:temp"); !errors.Is(err, ErrUnknownWorkflow) {
		t.Errorf("deleting twice: want ErrUnknownWorkflow, got %v", err)
	}
}

func TestRegisterWarnsAboutDanglingReference(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	_, warnings, err := e.Register([]workflow.Config{
		script("caller", workflow.Stage{Name: "call", Workflow: "ci:elsewhere"}),
	})
	if err != nil {
		t.Fatalf("a dangling reference must not fail registration: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "ci:elsewhere") {
		t.Errorf("want a warning naming the missing target, got %v", warnings)
	}
}

func TestUnknownWorkflowRun(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	if _, err := e.Run("ci:nosuch", runhistory.TriggerManual, true); !errors.Is(err, ErrUnknownWorkflow) {
		t.Errorf("want ErrUnknownWorkflow, got %v", err)
	}
}

func waitForPID(t *testing.T, e *Engine) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, run := range e.LiveRuns() {
			for _, st := range run.Stages {
				if st.PID != 0 {
					return
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no stage started within 5s")
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			var pid int
			if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no pid written to %s", path)
	return 0
}

// TestStopRunCancelsARunThatJustStarted pins the reason a run's context
// is created when it is claimed rather than when execute begins. A run
// is reachable by StopRun the moment it holds the single-flight slot; a
// cancel that was still a no-op at that point made StopRun silently do
// nothing and then block until the stage finished on its own.
func TestStopRunCancelsARunThatJustStarted(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	mustRegister(t, e, script("instant", stage("wait", "sleep 60")))

	run, err := e.Run("ci:instant", runhistory.TriggerManual, false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	start := time.Now()
	if err := e.StopRun(run.ID); err != nil {
		t.Fatalf("StopRun: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("StopRun blocked for %s; it did not cancel the run", elapsed)
	}

	final := e.List()
	if len(final) != 1 || final[0].Running {
		t.Errorf("workflow should no longer be running: %#v", final)
	}
}

// TestCancelledRunIsJournaledAsCancelled: a stopped run is a distinct
// outcome from a failed one, and the history has to say so.
func TestCancelledRunIsJournaledAsCancelled(t *testing.T) {
	e, home := newTestEngine(t, nil)
	mustRegister(t, e, script("stoppable", stage("wait", "sleep 60")))

	run, err := e.Run("ci:stoppable", runhistory.TriggerManual, false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := e.StopRun(run.ID); err != nil {
		t.Fatalf("StopRun: %v", err)
	}

	rec, ok, err := runhistory.NewStore(home).WorkflowRun(run.ID)
	if err != nil || !ok {
		t.Fatalf("cancelled run not journaled: ok=%v err=%v", ok, err)
	}
	if rec.Status != runhistory.StatusCancelled {
		t.Errorf("status: want %q, got %q", runhistory.StatusCancelled, rec.Status)
	}
}
