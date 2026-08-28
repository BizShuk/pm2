// Package workflow holds the subcommands of `pm2 workflow`.
package workflow

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/runhistory"
	wf "github.com/bizshuk/pm2/workflow"
	"github.com/spf13/cobra"
)

// ListCmd shows the declared workflows and their latest outcome.
var ListCmd = &cobra.Command{
	Use:   "list",
	Short: "List declared workflows and their latest run",
	Args:  cobra.NoArgs,
	RunE:  runList,
}

func runList(cmd *cobra.Command, _ []string) error {
	// model.SendRequest, not the auto-starting client: asking what is
	// declared must not spawn a daemon in order to answer.
	resp, err := model.SendRequest(cliruntime.SocketPath(), model.Request{Command: model.CmdWorkflowList})
	if err != nil {
		return fmt.Errorf("daemon not reachable: %w (start it with: pm2 daemon start)", err)
	}
	if !resp.OK {
		return fmt.Errorf("daemon error: %s", resp.Error)
	}

	var list []wf.Status
	if err := json.Unmarshal(resp.Payload, &list); err != nil {
		return fmt.Errorf("decode workflow list: %w", err)
	}
	renderWorkflowList(cmd.OutOrStdout(), list)
	return nil
}

func renderWorkflowList(out io.Writer, list []wf.Status) {
	if len(list) == 0 {
		fmt.Fprintln(out, "No workflows registered. Declare them in the workflows: block of your ecosystem file, then run: pm2 apply")
		return
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CATEGORY\tNAME\tSTAGES\tCRON\tSTATE\tLAST RUN")
	for _, st := range list {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\n",
			st.Category, st.Name, len(st.Stages), dash(st.Cron), stateOf(st), lastRunOf(st))
	}
	_ = w.Flush()
}

func stateOf(st wf.Status) string {
	if st.Running {
		return "running"
	}
	if st.LastStatus == "" {
		return "never run"
	}
	return string(st.LastStatus)
}

func lastRunOf(st wf.Status) string {
	if st.Running {
		return st.RunID
	}
	if st.LastRunAt.IsZero() {
		return "—"
	}
	return st.LastRunAt.Format("2006-01-02 15:04:05")
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// statusLabel keeps CLI wording aligned with the journal's own values.
func statusLabel(s runhistory.Status) string {
	if s == "" {
		return "—"
	}
	return string(s)
}
