package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/tui/views"
)

// listOptions are the user-facing flags for `pm2 list`.
type listOptions struct {
	noColor bool
}

var listOpts listOptions

// ListCmd renders all managed processes as a non-interactive table.
var ListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls", "status"},
	Short:   "List all managed processes (non-interactive)",
	Long: "Print a table of every process currently known to the daemon.\n" +
		"Aliased as `pm2 ls` and `pm2 status`. Column set matches the\n" +
		"former `pm2 m` wide table. Returns exit code 1 if the daemon is\n" +
		"unreachable, unlike `pm2 daemon status` which is an idempotent\n" +
		"probe (exit 0 even when the daemon is down).",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runList(&listOpts)
	},
}

func init() {
	ListCmd.Flags().BoolVar(&listOpts.noColor, "no-color", false, "Disable ANSI styling in the process table")
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

// renderList prints the former wide-dashboard table as a one-shot snapshot.
// Keeping rendering in tui/views gives `pm2 list` and the TUI one source of
// truth for borders, colors, column widths, and status presentation.
func renderList(out io.Writer, infos []process.ProcessInfo, opts *listOptions) {
	table := views.RenderProcessTable(infos, views.ProcessTableOptions{
		Width:   termWidth(),
		NoColor: opts.noColor,
	})
	fmt.Fprintln(out, table)
}

// termWidth returns the terminal width in columns. The fallback
// ladder is:
//  1. term.GetSize on stdout (real TTY only)
//  2. $COLUMNS env var
//  3. 120 (matches tui.Model default)
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
