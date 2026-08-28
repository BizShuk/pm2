package task

import (
	"fmt"
	"io"
	"strings"

	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/workflow"
)

// deleteEcosystemApps removes every task declared by an ecosystem file from
// the daemon registry. Each app is addressed by its exact
// "namespace:name" key so a delete never reaches a same-named app that
// belongs to another ecosystem file.
//
// An app the daemon does not know is reported and skipped rather than
// aborting the sweep: an ecosystem file routinely describes more apps than
// are currently registered (optional apps never resumed, a partial apply).
// The command only fails when nothing at all matched, so a wrong file or a
// stale config is still visible as an error.
func deleteEcosystemApps(apps []process.AppConfig, out io.Writer) error {
	if len(apps) == 0 {
		return fmt.Errorf("ecosystem file declares no apps to delete")
	}

	var (
		deleted int
		missing []string
	)
	for _, app := range apps {
		key := appSelectionKey(app)
		resp, err := model.SendRequest(cliruntime.SocketPath(), model.Request{
			Command: model.CmdDelete,
			Name:    key,
		})
		if err != nil {
			return err
		}
		if !resp.OK {
			missing = append(missing, key)
			fmt.Fprintf(out, "skipped: %s (%s)\n", key, resp.Error)
			continue
		}
		deleted++
		fmt.Fprintf(out, "deleted: %s\n", key)
	}

	if deleted == 0 {
		return fmt.Errorf("no task from this ecosystem file is registered: %s",
			strings.Join(missing, ", "))
	}
	return nil
}

// deleteEcosystemWorkflows is the workflow half of the --delete sweep.
// It follows the same posture as the app half: an unregistered workflow
// is skipped rather than fatal, because a file routinely declares more
// than the daemon currently holds.
//
// Unlike the app half it never fails on "nothing matched" — a file with
// apps but no workflows is entirely ordinary, and the app sweep already
// decides whether the file matched anything at all.
func deleteEcosystemWorkflows(workflows []workflow.Config, out io.Writer) {
	for _, cfg := range workflows {
		resp, err := model.SendRequest(cliruntime.SocketPath(), model.Request{
			Command:  model.CmdWorkflowDelete,
			Workflow: &model.WorkflowReq{Ref: cfg.Key()},
		})
		if err != nil {
			fmt.Fprintf(out, "skipped workflow: %s (%v)\n", cfg.Key(), err)
			continue
		}
		if !resp.OK {
			fmt.Fprintf(out, "skipped workflow: %s (%s)\n", cfg.Key(), resp.Error)
			continue
		}
		fmt.Fprintf(out, "deleted workflow: %s\n", cfg.Key())
	}
}
