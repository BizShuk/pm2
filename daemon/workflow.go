package daemon

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/workflow"
)

// This file is the daemon's half of the workflow feature: five thin
// Manager methods plus the one thing the engine needs back from the
// registry. Everything else lives in daemon/wfengine, which never
// imports this package.

// RegisterWorkflows upserts workflow definitions.
//
// Satisfies network.Manager (CmdWorkflowRegister).
func (pm *ProcessManager) RegisterWorkflows(cfgs []workflow.Config) ([]string, []string, error) {
	return pm.workflows.Register(cfgs)
}

// ListWorkflows returns every declared workflow with its latest outcome.
//
// Satisfies network.Manager (CmdWorkflowList).
func (pm *ProcessManager) ListWorkflows() []workflow.Status {
	return pm.workflows.List()
}

// RunWorkflow triggers one execution.
//
// Satisfies network.Manager (CmdWorkflowRun).
func (pm *ProcessManager) RunWorkflow(ref, trigger string, wait bool) (workflow.Run, error) {
	return pm.workflows.Run(ref, trigger, wait)
}

// DeleteWorkflow unregisters a workflow and disarms its schedule.
//
// Satisfies network.Manager (CmdWorkflowDelete).
func (pm *ProcessManager) DeleteWorkflow(ref string) error {
	return pm.workflows.Delete(ref)
}

// StopWorkflowRun cancels one run in flight.
//
// Satisfies network.Manager (CmdWorkflowStop).
func (pm *ProcessManager) StopWorkflowRun(runID string) error {
	return pm.workflows.StopRun(runID)
}

// LiveWorkflowRuns exposes the runs currently executing. The journal
// only holds finished runs, so this is the only view of an in-flight one.
func (pm *ProcessManager) LiveWorkflowRuns() []workflow.Run {
	return pm.workflows.LiveRuns()
}

// LookupTask resolves a `task:` stage reference to the static config to
// run once. It satisfies wfengine.TaskLookup.
//
// It reads value copies from Snapshot rather than handing back a live
// *ManagedProcess: reading fields off one outside the registry races
// with onProcessExit's writes, which is exactly the naked-read hazard
// the registry exists to prevent.
//
// Resolution never guesses. A reference containing ":" must match a key
// exactly; a bare name matches the default namespace first, then a
// unique name across namespaces. Two same-named tasks in different
// namespaces produce an error naming both, because silently picking one
// is a coin flip the caller cannot see.
func (pm *ProcessManager) LookupTask(ref string) (process.AppConfig, error) {
	infos := pm.reg.Snapshot()

	if strings.Contains(ref, ":") {
		for _, info := range infos {
			if cronKey(info.Namespace, info.Name) == ref {
				return info.AppConfig, nil
			}
		}
		return process.AppConfig{}, fmt.Errorf("task %q is not registered", ref)
	}

	var (
		matches []process.ProcessInfo
		keys    []string
	)
	for _, info := range infos {
		if info.Name != ref {
			continue
		}
		if info.Namespace == process.DefaultNamespace {
			return info.AppConfig, nil
		}
		matches = append(matches, info)
		keys = append(keys, cronKey(info.Namespace, info.Name))
	}

	switch len(matches) {
	case 0:
		return process.AppConfig{}, fmt.Errorf("task %q is not registered", ref)
	case 1:
		return matches[0].AppConfig, nil
	default:
		return process.AppConfig{}, fmt.Errorf("task %q is ambiguous: %s", ref, strings.Join(keys, ", "))
	}
}

// startWorkflows loads the persisted definitions and arms their
// schedules. A missing dump is an ordinary state — a daemon that has
// never applied a workflow — and is logged at info like the equivalent
// branch in startAutoResurrect.
func (pm *ProcessManager) startWorkflows() {
	if err := pm.workflows.Load(); err != nil {
		slog.Error("workflow load failed", "err", err, "homeDir", pm.homeDir)
		return
	}
	if n := len(pm.workflows.List()); n > 0 {
		slog.Info("workflows loaded", "count", n)
	}
}
