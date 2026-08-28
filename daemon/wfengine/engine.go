package wfengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/bizshuk/pm2/cron"
	"github.com/bizshuk/pm2/daemon/executor"
	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/runhistory"
	"github.com/bizshuk/pm2/workflow"
)

// MaxConcurrentRuns bounds how many workflow runs may be in flight at
// once. Single flight already stops one workflow from stacking on
// itself; this bounds the fan-out single flight cannot see — thirty-two
// distinct workflows each triggering the next.
const MaxConcurrentRuns = 32

var (
	// ErrRunInProgress is the one symbol a caller outside this package
	// needs to match on: the web layer turns it into HTTP 409.
	ErrRunInProgress = errors.New("workflow run already in progress")

	// ErrTooManyRuns reports the MaxConcurrentRuns ceiling.
	ErrTooManyRuns = errors.New("too many workflow runs in flight")

	// ErrUnknownWorkflow reports a reference no registered workflow matches.
	ErrUnknownWorkflow = errors.New("unknown workflow")
)

// TaskLookup is the entire contract wfengine needs from the daemon: given
// a task reference, the static config to run once.
//
// It returns a value copy, never a live registry entry — reading fields
// off a *ManagedProcess from here would race with the daemon's own exit
// handling.
type TaskLookup interface {
	LookupTask(ref string) (process.AppConfig, error)
}

// Engine owns the registered workflow definitions, their schedules, and
// the runs in flight.
type Engine struct {
	homeDir string
	tasks   TaskLookup
	exec    *executor.Executor
	history *runhistory.Store

	// scheduler is the engine's own, never the ProcessManager's.
	// stopProcess removes scheduler entries by a flat string key, so a
	// task in a namespace that collides with a workflow category would
	// let `pm2 task stop` silently disarm a workflow's schedule.
	scheduler *cron.Scheduler

	mu       sync.RWMutex
	defs     map[string]workflow.Config
	inflight map[string]*activeRun
}

// New returns an engine rooted at the pm2 state directory. Nothing is
// loaded or armed until Load runs.
func New(homeDir string, tasks TaskLookup, history *runhistory.Store) *Engine {
	return &Engine{
		homeDir:   homeDir,
		tasks:     tasks,
		exec:      executor.NewExecutor(homeDir),
		history:   history,
		scheduler: cron.New(),
		defs:      make(map[string]workflow.Config),
		inflight:  make(map[string]*activeRun),
	}
}

// Load reads the persisted definitions and arms their schedules. A
// missing dump is an empty engine, not an error — a daemon that has
// never applied a workflow has no file to read.
func (e *Engine) Load() error {
	data, err := os.ReadFile(workflow.DumpPath(e.homeDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read workflow dump: %w", err)
	}

	var cfgs []workflow.Config
	if err := json.Unmarshal(data, &cfgs); err != nil {
		return fmt.Errorf("parse workflow dump: %w", err)
	}

	e.mu.Lock()
	for _, c := range cfgs {
		c.Normalize("")
		e.defs[c.Key()] = c
	}
	defs := e.snapshotDefsLocked()
	e.mu.Unlock()

	e.armAll(defs)
	return nil
}

// Register upserts definitions, persists them, and re-arms every
// schedule. It returns the keys it registered plus non-fatal warnings.
//
// A declared cycle rejects the whole batch and changes nothing: half a
// workflow graph is not a useful intermediate state. A dangling
// reference or an unparsable cron expression is a warning instead — the
// target may live in another ecosystem file, and a bad schedule should
// not stop the other workflows in the same file from registering.
func (e *Engine) Register(cfgs []workflow.Config) ([]string, []string, error) {
	for _, c := range cfgs {
		if err := c.Validate(); err != nil {
			return nil, nil, err
		}
	}

	e.mu.Lock()
	merged := make(map[string]workflow.Config, len(e.defs)+len(cfgs))
	for k, v := range e.defs {
		merged[k] = v
	}
	for _, c := range cfgs {
		merged[c.Key()] = c
	}
	// The binding cycle check. The load-time one saw a single file; this
	// one sees every workflow the daemon knows.
	if err := workflow.CheckAcyclic(merged); err != nil {
		e.mu.Unlock()
		return nil, nil, err
	}
	e.defs = merged
	defs := e.snapshotDefsLocked()
	e.mu.Unlock()

	if err := e.persist(defs); err != nil {
		return nil, nil, err
	}

	registered := make([]string, 0, len(cfgs))
	for _, c := range cfgs {
		registered = append(registered, c.Key())
	}
	sort.Strings(registered)

	return registered, e.armAll(defs), nil
}

// Delete unregisters a workflow and disarms its schedule. Run history is
// left alone: a deleted workflow's past runs are exactly what someone
// asking "what happened last night" needs.
func (e *Engine) Delete(ref string) error {
	e.mu.Lock()
	key, ok := workflow.Resolve(e.defs, ref)
	if !ok {
		e.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrUnknownWorkflow, ref)
	}
	delete(e.defs, key)
	defs := e.snapshotDefsLocked()
	e.mu.Unlock()

	e.scheduler.Remove(key)
	return e.persist(defs)
}

// List returns every declared workflow with whatever is known about its
// most recent execution.
func (e *Engine) List() []workflow.Status {
	e.mu.RLock()
	defs := e.snapshotDefsLocked()
	live := make(map[string]string, len(e.inflight))
	for key, ar := range e.inflight {
		live[key] = ar.runID()
	}
	e.mu.RUnlock()

	out := make([]workflow.Status, 0, len(defs))
	for _, key := range workflow.Keys(defs) {
		st := workflow.Status{Config: defs[key]}
		if runID, running := live[key]; running {
			st.Running, st.RunID = true, runID
		} else if e.history != nil {
			if recs, err := e.history.RecentWorkflows(runhistory.Query{Name: key, Limit: 1}); err == nil && len(recs) > 0 {
				st.LastStatus, st.LastRunAt, st.RunID = recs[0].Status, recs[0].FinishedAt, recs[0].RunID
			}
		}
		out = append(out, st)
	}
	return out
}

// LiveRuns returns a snapshot of the runs currently executing. The
// journal only holds finished runs, so this is the only place an
// in-flight run is visible.
func (e *Engine) LiveRuns() []workflow.Run {
	e.mu.RLock()
	runs := make([]*activeRun, 0, len(e.inflight))
	for _, ar := range e.inflight {
		runs = append(runs, ar)
	}
	e.mu.RUnlock()

	out := make([]workflow.Run, 0, len(runs))
	for _, ar := range runs {
		out = append(out, ar.snapshot())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

// Run triggers one execution. wait blocks until the run reaches a
// terminal state; otherwise it returns as soon as the run is claimed,
// which is what a webhook needs.
func (e *Engine) Run(ref, trigger string, wait bool) (workflow.Run, error) {
	e.mu.RLock()
	key, ok := workflow.Resolve(e.defs, ref)
	if !ok {
		candidates := workflow.AmbiguousRef(e.defs, ref)
		e.mu.RUnlock()
		if len(candidates) > 1 {
			return workflow.Run{}, fmt.Errorf("workflow %q is ambiguous: %v", ref, candidates)
		}
		return workflow.Run{}, fmt.Errorf("%w: %s", ErrUnknownWorkflow, ref)
	}
	cfg := e.defs[key]
	e.mu.RUnlock()

	ar, err := e.claim(context.Background(), cfg, trigger, nil)
	if err != nil {
		return workflow.Run{}, err
	}

	go e.execute(cfg, ar)
	if !wait {
		return ar.snapshot(), nil
	}
	<-ar.done
	return ar.snapshot(), nil
}

// StopRun cancels one run in flight.
func (e *Engine) StopRun(runID string) error {
	e.mu.RLock()
	var target *activeRun
	for _, ar := range e.inflight {
		if ar.runID() == runID {
			target = ar
			break
		}
	}
	e.mu.RUnlock()

	if target == nil {
		return fmt.Errorf("no run in flight with id %s", runID)
	}
	target.cancel()
	<-target.done
	return nil
}

// Close stops the scheduler. Runs in flight are left alone: the daemon
// exits immediately after, and a half-cancelled run would record an
// outcome less honest than no record at all.
func (e *Engine) Close() { e.scheduler.Stop() }

// claim takes the single-flight slot for a workflow, or reports why it
// could not.
func (e *Engine) claim(ctx context.Context, cfg workflow.Config, trigger string, parent *activeRun) (*activeRun, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	key := cfg.Key()
	if existing, running := e.inflight[key]; running {
		return nil, fmt.Errorf("%w: %s (run %s)", ErrRunInProgress, key, existing.runID())
	}
	if len(e.inflight) >= MaxConcurrentRuns {
		return nil, fmt.Errorf("%w: %d already running", ErrTooManyRuns, len(e.inflight))
	}

	ar := newActiveRun(ctx, cfg, trigger, parent)
	e.inflight[key] = ar
	return ar, nil
}

func (e *Engine) release(key string, ar *activeRun) {
	e.mu.Lock()
	if e.inflight[key] == ar {
		delete(e.inflight, key)
	}
	e.mu.Unlock()
}

// snapshotDefsLocked copies the definition map. Callers must hold e.mu.
func (e *Engine) snapshotDefsLocked() map[string]workflow.Config {
	out := make(map[string]workflow.Config, len(e.defs))
	for k, v := range e.defs {
		out[k] = v
	}
	return out
}

func (e *Engine) persist(defs map[string]workflow.Config) error {
	list := make([]workflow.Config, 0, len(defs))
	for _, key := range workflow.Keys(defs) {
		list = append(list, defs[key])
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workflow dump: %w", err)
	}
	if err := os.MkdirAll(workflow.Dir(e.homeDir), 0o755); err != nil {
		return fmt.Errorf("create workflow directory: %w", err)
	}
	if err := os.WriteFile(workflow.DumpPath(e.homeDir), data, 0o644); err != nil {
		return fmt.Errorf("write workflow dump: %w", err)
	}
	return nil
}

// recordRun appends a finished run to the journal. Best-effort, like
// every other journal write in pm2: the run already happened, and
// failing it now would misreport what occurred.
func (e *Engine) recordRun(run workflow.Run) {
	if e.history == nil {
		return
	}
	if err := e.history.AppendWorkflow(run.Record()); err != nil {
		slog.Error("workflow run history append failed", "workflow", run.Key(), "run_id", run.ID, "err", err)
	}
}

func (e *Engine) runLogPath(name string) string {
	return filepath.Join(runhistory.WorkflowLogsDir(e.homeDir), name)
}
