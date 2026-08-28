package workflow

import (
	"encoding/json"
	"fmt"

	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/runhistory"
	wf "github.com/bizshuk/pm2/workflow"
	"github.com/spf13/cobra"
)

// RunCmd triggers one workflow run.
var RunCmd = &cobra.Command{
	Use:   "run <category:name|name>",
	Short: "Trigger one workflow run",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkflow,
	Example: `  pm2 workflow run ci:nightly
  pm2 workflow run nightly --wait`,
}

func init() {
	RunCmd.Flags().Bool("wait", false, "Block until the run finishes and print every stage")
}

func runWorkflow(cmd *cobra.Command, args []string) error {
	wait, err := cmd.Flags().GetBool("wait")
	if err != nil {
		return fmt.Errorf("read --wait: %w", err)
	}

	// This is the one workflow command that changes something, so it is
	// the one that may auto-start the daemon.
	client := cliruntime.NewCLIClient(cliruntime.SocketPath())
	resp, err := client.Send(model.Request{
		Command:  model.CmdWorkflowRun,
		Workflow: &model.WorkflowReq{Ref: args[0], Wait: wait},
	})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("daemon error: %s", resp.Error)
	}

	var run wf.Run
	if err := json.Unmarshal(resp.Payload, &run); err != nil {
		return fmt.Errorf("decode run: %w", err)
	}

	out := cmd.OutOrStdout()
	if !wait {
		fmt.Fprintf(out, "%s started (run %s)\nFollow it with: pm2 workflow show %s\n", run.Key(), run.ID, run.ID)
		return nil
	}

	renderRun(out, run)
	if run.Status != runhistory.StatusSuccess {
		// A failed workflow must fail the command: this is the exit code
		// a CI step or another script keys off.
		return fmt.Errorf("workflow %s: %s", run.Key(), statusLabel(run.Status))
	}
	return nil
}
