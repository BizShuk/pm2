package workflow

import (
	"time"

	"github.com/bizshuk/pm2/runhistory"
)

// StageRun is the in-memory state of one stage while a run is executing.
// Its durable counterpart is runhistory.StageRecord; the two are
// separate because a live stage has a PID and a record does not, and
// because runhistory must stay stdlib-only.
type StageRun struct {
	Index      int
	Name       string
	Kind       StageKind
	Ref        string
	Command    string
	Status     runhistory.Status
	ExitCode   int
	ExitKnown  bool
	Signal     string
	PID        int
	StartedAt  time.Time
	EndedAt    time.Time
	LogName    string
	ChildRunID string
	Error      string
}

// Run is the in-memory state of one workflow execution.
type Run struct {
	ID          string
	Category    string
	Name        string
	Trigger     string
	ParentRunID string
	Depth       int
	Params      map[string]string
	StartedAt   time.Time
	EndedAt     time.Time
	Status      runhistory.Status
	Error       string
	Stages      []StageRun
}

// Key is the workflow identity this run belongs to.
func (r Run) Key() string { return r.Category + ":" + r.Name }

// CurrentStage names the stage in flight, or "" when none is.
func (r Run) CurrentStage() string {
	for _, st := range r.Stages {
		if st.Status == "" {
			return st.Name
		}
	}
	return ""
}

// Record projects a finished run into the durable form. It is the single
// conversion point between the runtime and on-disk shapes.
func (r Run) Record() runhistory.WorkflowRecord {
	stages := make([]runhistory.StageRecord, 0, len(r.Stages))
	for _, st := range r.Stages {
		stages = append(stages, runhistory.StageRecord{
			Name:       st.Name,
			Kind:       string(st.Kind),
			Ref:        st.Ref,
			Status:     st.Status,
			ExitCode:   runhistory.ExitCodeOf(st.ExitCode, st.ExitKnown),
			Signal:     st.Signal,
			StartedAt:  st.StartedAt,
			DurationMS: durationMS(st.StartedAt, st.EndedAt),
			Log:        st.LogName,
			ChildRunID: st.ChildRunID,
			Error:      st.Error,
		})
	}
	return runhistory.WorkflowRecord{
		RunID:       r.ID,
		Workflow:    r.Key(),
		Category:    r.Category,
		Name:        r.Name,
		Trigger:     r.Trigger,
		ParentRunID: r.ParentRunID,
		Depth:       r.Depth,
		StartedAt:   r.StartedAt,
		FinishedAt:  r.EndedAt,
		DurationMS:  durationMS(r.StartedAt, r.EndedAt),
		Status:      r.Status,
		Params:      r.Params,
		Stages:      stages,
		Error:       r.Error,
	}
}

func durationMS(start, end time.Time) int64 {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}

// Status is a declared workflow plus whatever is known about its most
// recent execution — the shape `pm2 workflow list` and the web UI
// both render.
type Status struct {
	Config
	Running    bool              `json:"running"`
	RunID      string            `json:"run_id,omitempty"`
	LastStatus runhistory.Status `json:"last_status,omitempty"`
	LastRunAt  time.Time         `json:"last_run_at,omitzero"`
}
