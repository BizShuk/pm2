package wizard

import (
	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/workflow"
)

// Ecosystem is the whole document the wizard authors: the `apps:` block
// and the `workflows:` block of one ecosystem file.
//
// It exists so the wizard's collect -> merge -> render -> write pipeline
// carries one value instead of a widening parameter list. Every stage of
// that pipeline sees both blocks, because they are not independent: a
// `task:` stage names an app, so validating or merging one half without
// the other would let the wizard write a file it knows is broken.
type Ecosystem struct {
	Apps      []process.AppConfig
	Workflows []workflow.Config
}

// TaskKeys returns the "<namespace>:<name>" identity of every app in the
// document, in declaration order. That is the exact form a `task:` stage
// resolves against in the daemon registry, which is why the stage picker
// offers these strings rather than bare names.
func (e Ecosystem) TaskKeys() []string {
	keys := make([]string, 0, len(e.Apps))
	for _, app := range e.Apps {
		app.Normalize("")
		if app.Name == "" {
			continue
		}
		namespace := app.Namespace
		if namespace == "" {
			namespace = process.DefaultNamespace
		}
		keys = append(keys, namespace+":"+app.Name)
	}
	return keys
}

// WorkflowKeys returns the "<category>:<name>" identity of every
// workflow in the document, in declaration order.
func (e Ecosystem) WorkflowKeys() []string {
	keys := make([]string, 0, len(e.Workflows))
	for _, wf := range e.Workflows {
		if wf.Name == "" {
			continue
		}
		category := wf.Category
		if category == "" {
			category = workflow.DefaultCategory
		}
		keys = append(keys, category+":"+wf.Name)
	}
	return keys
}
