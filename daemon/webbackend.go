package daemon

import (
	"github.com/bizshuk/pm2/daemon/web"
	"github.com/bizshuk/pm2/process"
)

// This file adapts ProcessManager to web.Backend. The conversion lives
// here, on the daemon side of the interface, so daemon/web can compile
// and be tested without importing the workflow package at all.

// ListTasks satisfies web.Backend.
func (pm *ProcessManager) ListTasks() []process.ProcessInfo { return pm.ListAll() }

// DaemonStatus satisfies web.Backend.
func (pm *ProcessManager) DaemonStatus() process.DaemonInfo { return pm.Status() }

// WebWorkflows converts the domain type into the web projection. It
// cannot be called ListWorkflows: network.Manager already claims that
// name for the domain shape, and the two interfaces genuinely want
// different things — one returns workflow.Status, the other a view with
// no secrets in it.
func (pm *ProcessManager) WebWorkflows() []web.WorkflowSummary {
	list := pm.workflows.List()
	out := make([]web.WorkflowSummary, 0, len(list))
	for _, st := range list {
		stages := make([]string, 0, len(st.Stages))
		for _, stage := range st.Stages {
			stages = append(stages, stage.Name)
		}
		out = append(out, web.WorkflowSummary{
			Key: st.Key(), Category: st.Category, Name: st.Name,
			Cron: st.Cron, Stages: stages,
			Running: st.Running, RunID: st.RunID,
			LastStatus: string(st.LastStatus), LastRunAt: st.LastRunAt,
		})
	}
	return out
}

// WebActiveRuns projects the runs in flight.
func (pm *ProcessManager) WebActiveRuns() []web.RunSummary {
	runs := pm.workflows.LiveRuns()
	out := make([]web.RunSummary, 0, len(runs))
	for _, run := range runs {
		out = append(out, web.RunSummary{
			RunID: run.ID, Workflow: run.Key(), Trigger: run.Trigger,
			Stage: run.CurrentStage(), StartedAt: run.StartedAt, Running: true,
		})
	}
	return out
}

// WebTriggerWorkflow starts a run and returns as soon as it is claimed.
// The webhook must not hold a connection open for the length of a run.
func (pm *ProcessManager) WebTriggerWorkflow(name, trigger string) (web.RunSummary, error) {
	run, err := pm.workflows.Run(name, trigger, false)
	if err != nil {
		return web.RunSummary{}, err
	}
	return web.RunSummary{
		RunID: run.ID, Workflow: run.Key(), Trigger: run.Trigger,
		StartedAt: run.StartedAt, Running: true,
	}, nil
}

// webBackend adapts ProcessManager's method names to web.Backend's.
// A thin named type rather than renaming the Manager methods: the two
// interfaces genuinely want different things from ListWorkflows — one
// returns the domain type, the other a projection with no secrets in it.
type webBackend struct{ pm *ProcessManager }

func (b webBackend) ListTasks() []process.ProcessInfo     { return b.pm.ListTasks() }
func (b webBackend) DaemonStatus() process.DaemonInfo     { return b.pm.DaemonStatus() }
func (b webBackend) ListWorkflows() []web.WorkflowSummary { return b.pm.WebWorkflows() }
func (b webBackend) ActiveRuns() []web.RunSummary         { return b.pm.WebActiveRuns() }

func (b webBackend) TriggerWorkflow(name, trigger string) (web.RunSummary, error) {
	return b.pm.WebTriggerWorkflow(name, trigger)
}

var _ web.Backend = webBackend{}
