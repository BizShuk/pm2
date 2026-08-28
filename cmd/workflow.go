package cmd

import (
	"fmt"

	workflowcmd "github.com/bizshuk/pm2/cmd/workflow"
	"github.com/spf13/cobra"
)

// WorkflowCmd groups the workflow commands under `pm2 workflow`.
//
// No short alias. The alias table is the product's, not a pattern to
// extend by default — `pm2 gpu` has none either — and `pm2 w` is
// already the wizard.
//
// The four verbs split along one line, the same one `pm2 logs` and
// `pm2 logs monitor` already draw: making something happen needs the
// daemon, what already happened is a file.
//
//   - list.go — declared workflows and their latest outcome (RPC)
//   - run.go  — trigger one run (RPC)
//   - runs.go — run history, straight off disk
//   - show.go — one run in detail, straight off disk
var WorkflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Run and inspect multi-stage workflows",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		return fmt.Errorf("pm2 workflow requires a subcommand (list | run | runs | show)")
	},
}

func init() {
	WorkflowCmd.AddCommand(
		workflowcmd.ListCmd,
		workflowcmd.RunCmd,
		workflowcmd.RunsCmd,
		workflowcmd.ShowCmd,
	)
}
