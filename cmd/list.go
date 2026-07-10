package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
)

// listColumns mirrors tui/views/list.go's listColumns so `pm2 list`
// reads the same as `pm2 monit`'s wide table. The widths are
// minimal render hints — text/tabwriter actually measures cell
// widths itself; these are only used by the width-based dropping
// logic and the cosmetic alignment hints.
var listColumns = []listColumn{
	{name: "id", width: 3, rightAlign: true},
	{name: "namespace", width: 10},
	{name: "name", width: 0}, // dynamic, filled by termWidth
	{name: "version", width: 8},
	{name: "pid", width: 6, rightAlign: true},
	{name: "uptime", width: 8, rightAlign: true},
	{name: "↺", width: 3, rightAlign: true},
	{name: "status", width: 9},
	{name: "cpu", width: 6, rightAlign: true},
	{name: "mem", width: 8, rightAlign: true},
	{name: "user", width: 8},
	{name: "cron", width: 10},
	{name: "last exec", width: 19},
}

type listColumn struct {
	name       string
	width      int // base width for shrink / grow calculation
	rightAlign bool
}

// Width-based field dropping thresholds. Below listWideWidth the
// three "tail" columns (user / cron / last exec) are dropped so
// the table fits in 100-col terminals. Below listNarrowWidth
// version is also dropped so the table fits in 80-col terminals.
// These match the spirit of the monit view, which always renders
// all 13 columns in TUI but here we are output-stream-friendly.
const (
	listWideWidth   = 100
	listNarrowWidth = 80
)

// listOptions are the user-facing flags for `pm2 list`. Kept as a
// struct so adding a flag later (e.g. --json) doesn't churn the
// cobra.Command signature.
type listOptions struct {
	noColor bool
}

func newListCmd() *cobra.Command {
	opts := &listOptions{}
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls", "status"},
		Short:   "List all managed processes (non-interactive)",
		Long: "Print a table of every process currently known to the daemon.\n" +
			"Aliased as `pm2 ls` and `pm2 status`. Column set matches the\n" +
			"`pm2 monit` wide table. Returns exit code 1 if the daemon is\n" +
			"unreachable, unlike `pm2 daemon status` which is an idempotent\n" +
			"probe (exit 0 even when the daemon is down).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(opts)
		},
	}
	cmd.Flags().BoolVar(&opts.noColor, "no-color", false, "Disable ANSI color in the table header (currently a no-op — tabwriter has no colour awareness)")
	return cmd
}

func runList(opts *listOptions) error {
	client := NewCLIClient(socketPath())

	// Send folds the dial+auto-respawn logic; we still own the
	// resp.OK check here so the dial-failure path can be rewritten
	// into a more helpful hint for `pm2 list` users. A live daemon
	// that returns OK=false carries a useful resp.Error, which we
	// preserve as-is.
	resp, err := client.Send(model.Request{Command: model.CmdList})
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon not running, start with 'pm2 daemon start': %v\n", err)
		return err
	}
	if !resp.OK {
		return fmt.Errorf("daemon: %s", resp.Error)
	}

	var infos []process.ProcessInfo
	if err := json.Unmarshal(resp.Payload, &infos); err != nil {
		return fmt.Errorf("decode list: %w", err)
	}

	renderList(os.Stdout, infos, opts)
	return nil
}

// renderList prints the table using text/tabwriter. The
// width-based field dropping is driven by `termWidth()` so CI logs
// and pipelines (no TTY, no COLUMNS) get the full 13-column
// layout.
//
// text/tabwriter is the stdlib tab-aligned writer; we set 1-cell
// padding around each cell and 2-space column separator. No border
// — matches the existing `pm2 list` look used by other CLIs in
// this repo.
func renderList(out *os.File, infos []process.ProcessInfo, opts *listOptions) {
	_ = opts // kept for future --no-color behaviour; stdlib tabwriter
	// has no ANSI awareness so there is nothing to toggle here yet.

	width := termWidth()
	cols := buildListColumns(width)
	headers, rows := buildListTable(infos, cols)

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, h := range headers {
		fmt.Fprint(tw, h+"\t")
	}
	fmt.Fprintln(tw)
	for _, row := range rows {
		for _, cell := range row {
			fmt.Fprint(tw, cell+"\t")
		}
		fmt.Fprintln(tw)
	}
	_ = tw.Flush()
}

// buildListColumns returns the active set of columns given the
// terminal width. The order matches monit's wide table; tail
// columns are dropped first when the terminal is narrow.
func buildListColumns(width int) []listColumn {
	cols := make([]listColumn, len(listColumns))
	copy(cols, listColumns)

	if width > 0 && width < listWideWidth {
		// Drop user / cron / last exec (last three columns) below
		// listWideWidth.
		cols = cols[:len(cols)-3]
	}
	if width > 0 && width < listNarrowWidth {
		// Drop version (now the last column) below listNarrowWidth.
		cols = cols[:len(cols)-1]
	}
	return cols
}

// buildListTable returns the header row and the data rows for the
// given columns. Cells come from the shared process/format.go
// helpers so the values match monit exactly.
func buildListTable(infos []process.ProcessInfo, cols []listColumn) ([]string, [][]string) {
	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = c.name
	}

	rows := make([][]string, 0, len(infos))
	for _, p := range infos {
		row := make([]string, len(cols))
		for i, c := range cols {
			row[i] = cellFor(p, c.name)
		}
		rows = append(rows, row)
	}
	return headers, rows
}

// cellFor returns the rendered text for a single column. Keeps the
// switch in one place so the column set stays easy to extend.
func cellFor(p process.ProcessInfo, name string) string {
	switch name {
	case "id":
		return strconv.Itoa(p.ID)
	case "namespace":
		return process.NamespaceOrDefault(p.Namespace)
	case "name":
		return p.Name
	case "version":
		return process.VersionOrDash(p.Version)
	case "pid":
		return process.PIDOrDash(p.PID)
	case "uptime":
		return process.ShortUptime(p)
	case "↺":
		return strconv.Itoa(p.Restarts)
	case "status":
		return string(p.Status)
	case "cpu":
		return process.CPUPercent(p)
	case "mem":
		return process.MemCell(p)
	case "user":
		return process.UserOrDash(p.User)
	case "cron":
		return process.CronOrDash(p.Cron)
	case "last exec":
		return process.LastExec(p)
	default:
		return ""
	}
}

// termWidth returns the terminal width in columns. The fallback
// ladder is:
//  1. term.GetSize on stdout (real TTY only)
//  2. $COLUMNS env var
//  3. 120 (matches tui.Model default)
//
// Returns 0 (which buildListColumns treats as "use all columns")
// when none of the three sources yield a positive value.
func termWidth() int {
	if fd := int(os.Stdout.Fd()); term.IsTerminal(fd) {
		if w, _, err := term.GetSize(fd); err == nil && w > 0 {
			return w
		}
	}
	if v := os.Getenv("COLUMNS"); v != "" {
		if w, err := strconv.Atoi(v); err == nil && w > 0 {
			return w
		}
	}
	return 120
}

// keep the time import used in case future column needs a Now
// reference (e.g. cronNext). The blank assignment compiles to a
// no-op but keeps the import honest.
var _ = time.Now
