package workflow

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	"github.com/bizshuk/pm2/runhistory"
	"github.com/spf13/cobra"
)

// RunsCmd lists past workflow runs.
//
// It reads the journal directly and never opens the socket: history is a
// file, so it stays readable with the daemon down and outlives the
// workflow that produced it. This is the same split `pm2 logs` and
// `pm2 logs monitor` already draw.
var RunsCmd = &cobra.Command{
	Use:   "runs [category:name|name]",
	Short: "List past workflow runs (reads the journal; no daemon needed)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runRuns,
}

func init() {
	RunsCmd.Flags().Int("limit", 20, "Maximum number of runs to show")
	RunsCmd.Flags().String("status", "", "Only show runs with this status (success | failed | skipped | cancelled)")
}

func runRuns(cmd *cobra.Command, args []string) error {
	limit, err := cmd.Flags().GetInt("limit")
	if err != nil {
		return fmt.Errorf("read --limit: %w", err)
	}
	status, err := cmd.Flags().GetString("status")
	if err != nil {
		return fmt.Errorf("read --status: %w", err)
	}

	query := runhistory.Query{Limit: limit, Status: runhistory.Status(status)}
	if len(args) > 0 {
		query.Name = args[0]
	}

	records, err := cliruntime.RunHistoryStore().RecentWorkflows(query)
	if err != nil {
		return fmt.Errorf("read workflow history: %w", err)
	}
	renderRuns(cmd.OutOrStdout(), records)
	return nil
}

func renderRuns(out io.Writer, records []runhistory.WorkflowRecord) {
	if len(records) == 0 {
		fmt.Fprintln(out, "No workflow runs recorded yet.")
		return
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "RUN ID\tWORKFLOW\tTRIGGER\tSTATUS\tDURATION\tFINISHED")
	for _, rec := range records {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			rec.RunID, rec.Workflow, rec.Trigger, statusLabel(rec.Status),
			humanMS(rec.DurationMS), rec.FinishedAt.Format("2006-01-02 15:04:05"))
	}
	_ = w.Flush()
}

func humanMS(ms int64) string {
	if ms <= 0 {
		return "—"
	}
	return (time.Duration(ms) * time.Millisecond).Round(time.Millisecond).String()
}
