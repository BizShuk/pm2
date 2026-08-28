package workflow

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	"github.com/bizshuk/pm2/runhistory"
	wf "github.com/bizshuk/pm2/workflow"
	"github.com/spf13/cobra"
)

// ShowCmd prints one run in detail. Like `runs`, it reads the journal
// rather than the socket.
var ShowCmd = &cobra.Command{
	Use:   "show <run-id>",
	Short: "Show one workflow run and its stages (reads the journal; no daemon needed)",
	Args:  cobra.ExactArgs(1),
	RunE:  runShow,
}

func init() {
	ShowCmd.Flags().Bool("logs", false, "Print each stage's captured output")
}

func runShow(cmd *cobra.Command, args []string) error {
	withLogs, err := cmd.Flags().GetBool("logs")
	if err != nil {
		return fmt.Errorf("read --logs: %w", err)
	}

	store := cliruntime.RunHistoryStore()
	rec, ok, err := store.WorkflowRun(args[0])
	if err != nil {
		return fmt.Errorf("read workflow history: %w", err)
	}
	if !ok {
		return fmt.Errorf("no run recorded with id %s (a run still in flight is not journaled until it finishes — see: pm2 workflow list)", args[0])
	}

	out := cmd.OutOrStdout()
	renderRecord(out, rec)

	if withLogs {
		for _, st := range rec.Stages {
			if st.Log == "" {
				continue
			}
			fmt.Fprintf(out, "\n─── %s ───\n", st.Name)
			printStageLog(out, store.StageLogPath(rec.Workflow, rec.RunID, st.Name))
		}
	}
	return nil
}

func renderRecord(out io.Writer, rec runhistory.WorkflowRecord) {
	fmt.Fprintf(out, "%s  %s\n", rec.Workflow, rec.RunID)
	fmt.Fprintf(out, "trigger: %s    status: %s    duration: %s\n",
		rec.Trigger, statusLabel(rec.Status), humanMS(rec.DurationMS))
	fmt.Fprintf(out, "started: %s    finished: %s\n",
		rec.StartedAt.Format("2006-01-02 15:04:05"), rec.FinishedAt.Format("2006-01-02 15:04:05"))
	if rec.ParentRunID != "" {
		fmt.Fprintf(out, "called by run %s (depth %d)\n", rec.ParentRunID, rec.Depth)
	}
	if rec.Error != "" {
		fmt.Fprintf(out, "error: %s\n", rec.Error)
	}
	fmt.Fprintln(out)

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "#\tSTAGE\tKIND\tSTATUS\tEXIT\tDURATION")
	for i, st := range rec.Stages {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n",
			i+1, st.Name, dash(st.Kind), statusLabel(st.Status), exitLabel(st), humanMS(st.DurationMS))
	}
	_ = w.Flush()

	for _, st := range rec.Stages {
		if st.Error != "" {
			fmt.Fprintf(out, "\nstage %q: %s\n", st.Name, st.Error)
		}
		if st.ChildRunID != "" {
			fmt.Fprintf(out, "stage %q ran workflow %s — see: pm2 workflow show %s\n", st.Name, st.Ref, st.ChildRunID)
		}
	}
}

// exitLabel keeps "no exit code" visibly different from "exited 0": a
// stage that never started and one that succeeded are not the same
// thing, and a bare 0 would say they were.
func exitLabel(st runhistory.StageRecord) string {
	if st.Signal != "" {
		return st.Signal
	}
	if st.ExitCode == nil {
		return "—"
	}
	return fmt.Sprintf("%d", *st.ExitCode)
}

func printStageLog(out io.Writer, path string) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(out, "(log unavailable: %v)\n", err)
		return
	}
	defer f.Close()
	if _, err := io.Copy(out, f); err != nil {
		fmt.Fprintf(out, "(log truncated: %v)\n", err)
	}
}

// renderRun prints a run the daemon just returned. It reuses the
// journal renderer through Record() so `run --wait` and `show` cannot
// describe the same run differently.
func renderRun(out io.Writer, run wf.Run) {
	renderRecord(out, run.Record())
}
