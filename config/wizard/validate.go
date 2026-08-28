package wizard

import (
	"fmt"
	"io"
	"slices"

	"github.com/bizshuk/pm2/workflow"
)

// validateDocument rejects a workflow set the daemon would refuse:
// a nameless workflow, a stage that is not exactly one of
// script/task/workflow, a duplicate key, or a declared cycle. It runs
// before the preview, so the user is never shown a file that
// `pm2 apply` cannot load.
func validateDocument(doc Ecosystem) error {
	return workflow.ValidateAll(doc.Workflows)
}

// warnDanglingRefs reports references that resolve to nothing in this
// file. It is a warning and never an error: a task or a workflow may be
// registered from another ecosystem file, which is exactly why the
// daemon's own registration check — not this one — is the binding one.
func warnDanglingRefs(errOut io.Writer, doc Ecosystem) {
	defs := make(map[string]workflow.Config, len(doc.Workflows))
	for _, wf := range doc.Workflows {
		wf.Normalize("")
		defs[wf.Key()] = wf
	}
	for _, ref := range workflow.DanglingRefs(defs) {
		fmt.Fprintf(
			errOut,
			"  (warning: workflow %s stage %d references workflow %q, which this file does not declare)\n",
			ref.Workflow, ref.Stage, ref.Target,
		)
	}

	keys := doc.TaskKeys()
	names := make([]string, 0, len(doc.Apps))
	for _, key := range keys {
		_, name := workflow.ParseKey(key)
		names = append(names, name)
	}
	for _, wf := range doc.Workflows {
		wf.Normalize("")
		for i, st := range wf.Stages {
			if st.Kind() != workflow.StageTask {
				continue
			}
			if slices.Contains(keys, st.Task) || slices.Contains(names, st.Task) {
				continue
			}
			fmt.Fprintf(
				errOut,
				"  (warning: workflow %s stage %d references task %q, which this file does not declare)\n",
				wf.Key(), i+1, st.Task,
			)
		}
	}
}
