package wizard

import "github.com/bizshuk/pm2/workflow"

// renderedStage and renderedWorkflow are the user-authored projection of
// a workflow, mirroring renderedApp. Runtime-stamped fields — ConfigFile
// and BaseEnv, the CLI's snapshot of the operator's shell environment —
// are absent by construction: the wizard writes a file people read and
// commit, not a dump.
type renderedStage struct {
	Name     string            `json:"name"`
	Script   string            `json:"script,omitempty"`
	Args     []string          `json:"args,omitempty"`
	Task     string            `json:"task,omitempty"`
	Workflow string            `json:"workflow,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	CWD      string            `json:"cwd,omitempty"`
	Timeout  string            `json:"timeout,omitempty"`
}

type renderedWorkflow struct {
	Name     string            `json:"name"`
	Category string            `json:"category"`
	Cron     string            `json:"cron,omitempty"`
	Timeout  string            `json:"timeout,omitempty"`
	CWD      string            `json:"cwd,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	Stages   []renderedStage   `json:"stages"`
}

func workflowForRender(wf workflow.Config) renderedWorkflow {
	// Normalize with an empty base directory fills the category and the
	// stage names without resolving any path, so a relative script stays
	// as the user typed it.
	wf.Normalize("")

	stages := make([]renderedStage, len(wf.Stages))
	for i, st := range wf.Stages {
		cwd := st.CWD
		// Normalize derives a stage CWD from the workflow's, so writing
		// it back would turn a default into a pin that survives a later
		// edit of the workflow's own cwd.
		if cwd == wf.CWD {
			cwd = ""
		}
		stages[i] = renderedStage{
			Name:     st.Name,
			Script:   st.Script,
			Args:     st.Args,
			Task:     st.Task,
			Workflow: st.Workflow,
			Env:      st.Env,
			CWD:      cwd,
			Timeout:  st.Timeout,
		}
	}

	return renderedWorkflow{
		Name:     wf.Name,
		Category: wf.Category,
		Cron:     wf.Cron,
		Timeout:  wf.Timeout,
		CWD:      wf.CWD,
		Env:      wf.Env,
		Stages:   stages,
	}
}
