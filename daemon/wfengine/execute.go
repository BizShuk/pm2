package wfengine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bizshuk/pm2/runhistory"
	"github.com/bizshuk/pm2/workflow"
)

// timeNow is a seam for tests; production always uses the wall clock.
var timeNow = time.Now

// activeRun is one execution in flight.
type activeRun struct {
	mu    sync.Mutex
	run   workflow.Run
	chain []string // ancestry, root first — the runtime cycle guard

	// ctx and cancel are created when the run is claimed, not when
	// execute starts. A run is reachable by StopRun the moment it holds
	// the single-flight slot, and a cancel that is still a no-op at that
	// point would make StopRun silently do nothing and then block until
	// the stage finished on its own.
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func newActiveRun(parentCtx context.Context, cfg workflow.Config, trigger string, parent *activeRun) *activeRun {
	now := timeNow()
	run := workflow.Run{
		ID:        runhistory.NewRunID(now),
		Category:  cfg.Category,
		Name:      cfg.Name,
		Trigger:   trigger,
		StartedAt: now,
		Status:    "",
		Stages:    make([]workflow.StageRun, 0, len(cfg.Stages)),
	}
	for i, st := range cfg.Stages {
		run.Stages = append(run.Stages, workflow.StageRun{
			Index: i, Name: st.Name, Kind: st.Kind(), Ref: st.Ref(),
		})
	}

	chain := []string{cfg.Key()}
	if parent != nil {
		snap := parent.snapshot()
		run.ParentRunID = snap.ID
		run.Depth = snap.Depth + 1
		chain = append(append([]string{}, parent.ancestry()...), cfg.Key())
	}

	ctx, cancel := context.WithCancel(parentCtx)
	return &activeRun{run: run, chain: chain, ctx: ctx, cancel: cancel, done: make(chan struct{})}
}

func (a *activeRun) runID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.run.ID
}

func (a *activeRun) ancestry() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string{}, a.chain...)
}

func (a *activeRun) snapshot() workflow.Run {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.run
	out.Stages = append([]workflow.StageRun{}, a.run.Stages...)
	return out
}

// publishStage makes an in-flight stage visible to LiveRuns without
// marking it finished — Status stays empty until the stage ends, which
// is what CurrentStage keys on.
func (a *activeRun) publishStage(idx int, st workflow.StageRun) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if idx >= 0 && idx < len(a.run.Stages) {
		a.run.Stages[idx] = st
	}
}

func (a *activeRun) update(fn func(*workflow.Run)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	fn(&a.run)
}

// execute runs the stages in order and journals the outcome.
func (e *Engine) execute(cfg workflow.Config, ar *activeRun) {
	ctx := ar.ctx

	defer func() {
		ar.cancel()
		// The release and the journal write both happen even if a stage
		// panicked: a wedged single-flight slot would disable this
		// workflow for the lifetime of the daemon, including its cron.
		if r := recover(); r != nil {
			ar.update(func(run *workflow.Run) {
				run.Status = runhistory.StatusFailed
				run.Error = fmt.Sprintf("panic: %v", r)
				run.EndedAt = timeNow()
			})
			slog.Error("workflow run panicked", "workflow", cfg.Key(), "run_id", ar.runID(), "panic", r)
		}
		e.release(cfg.Key(), ar)
		e.recordRun(ar.snapshot())
		close(ar.done)
	}()

	status := runhistory.StatusSuccess
	var runErr string

	for i := range cfg.Stages {
		if ctx.Err() != nil {
			status = runhistory.StatusCancelled
			runErr = "run cancelled"
			e.markRemaining(ar, i, runhistory.StatusCancelled)
			break
		}

		result := e.runStage(ctx, cfg, cfg.Stages[i], ar, i)
		ar.update(func(run *workflow.Run) { run.Stages[i] = result })

		if result.Status == runhistory.StatusSuccess {
			continue
		}

		status = result.Status
		runErr = result.Error
		if runErr == "" {
			runErr = fmt.Sprintf("stage %d (%q) failed with exit code %d", i+1, result.Name, result.ExitCode)
		}
		// Remaining stages are recorded as skipped, not omitted: a
		// reader has to see the whole declared sequence to know where it
		// stopped, and an absent stage looks like a config that never
		// declared it.
		e.markRemaining(ar, i+1, runhistory.StatusSkipped)
		break
	}

	ar.update(func(run *workflow.Run) {
		run.Status = status
		run.Error = runErr
		run.EndedAt = timeNow()
	})
}

func (e *Engine) markRemaining(ar *activeRun, from int, status runhistory.Status) {
	ar.update(func(run *workflow.Run) {
		for i := from; i < len(run.Stages); i++ {
			if run.Stages[i].Status == "" {
				run.Stages[i].Status = status
			}
		}
	})
}

// runStage dispatches one stage by kind.
func (e *Engine) runStage(ctx context.Context, cfg workflow.Config, st workflow.Stage, ar *activeRun, idx int) workflow.StageRun {
	result := workflow.StageRun{
		Index: idx, Name: st.Name, Kind: st.Kind(), Ref: st.Ref(),
		StartedAt: timeNow(),
	}
	ar.publishStage(idx, result)

	switch st.Kind() {
	case workflow.StageWorkflow:
		e.runNested(ctx, st, ar, &result)
	case workflow.StageTask:
		appCfg, err := e.tasks.LookupTask(st.Task)
		if err != nil {
			result.Status = runhistory.StatusFailed
			result.Error = err.Error()
			break
		}
		// A task stage runs the task's command exactly once. Instances,
		// Cron, CronRestart, Watch, MaxRestarts, Paused, and Optional all
		// describe how a task is *supervised*, which has no meaning for
		// a single execution — and honouring any of them here would turn
		// a stage into a second registration of an existing task.
		e.execStage(ctx, stageExec{
			Script:  appCfg.Script,
			Args:    appCfg.Args,
			Env:     appCfg.Env,
			BaseEnv: appCfg.BaseEnv,
			CWD:     appCfg.CWD,
			Timeout: cfg.TimeoutDuration(st),
		}, cfg, ar, idx, &result)
	default:
		e.execStage(ctx, stageExec{
			Script:  st.Script,
			Args:    st.Args,
			Env:     mergeEnv(cfg.Env, st.Env),
			BaseEnv: cfg.BaseEnv,
			CWD:     st.CWD,
			Timeout: cfg.TimeoutDuration(st),
		}, cfg, ar, idx, &result)
	}

	result.EndedAt = timeNow()
	return result
}

// runNested executes a child workflow inline. The parent blocks: the
// stages are linear, so the child's outcome is the stage's outcome.
func (e *Engine) runNested(ctx context.Context, st workflow.Stage, ar *activeRun, result *workflow.StageRun) {
	e.mu.RLock()
	childKey, ok := workflow.Resolve(e.defs, st.Workflow)
	childCfg := e.defs[childKey]
	e.mu.RUnlock()

	if !ok {
		result.Status = runhistory.StatusFailed
		result.Error = fmt.Sprintf("%v: %s", ErrUnknownWorkflow, st.Workflow)
		return
	}

	chain := ar.ancestry()
	for _, seen := range chain {
		if seen == childKey {
			result.Status = runhistory.StatusFailed
			result.Error = "workflow cycle: " + strings.Join(append(chain, childKey), " -> ")
			return
		}
	}
	if len(chain) >= workflow.MaxNestingDepth {
		result.Status = runhistory.StatusFailed
		result.Error = fmt.Sprintf("workflow nesting depth %d exceeded: %s",
			workflow.MaxNestingDepth, strings.Join(chain, " -> "))
		return
	}

	// The child's context descends from the parent's, so cancelling the
	// root cancels the whole tree.
	child, err := e.claim(ctx, childCfg, runhistory.TriggerNested, ar)
	if err != nil {
		result.Status = runhistory.StatusFailed
		result.Error = err.Error()
		return
	}

	e.execute(childCfg, child)
	<-child.done

	snap := child.snapshot()
	result.ChildRunID = snap.ID
	result.Status = snap.Status
	result.Error = snap.Error
}

// mergeEnv layers the stage's own variables over the workflow's.
func mergeEnv(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}
