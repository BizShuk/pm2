// Package dashboard owns the `pm2 dashboard` command tree: the
// interactive activity monitor and its non-interactive snapshot emitter.
package dashboard

import (
	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	"github.com/bizshuk/pm2/tui/dashboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// Cmd opens the system activity monitor.
//
// It is a peer of `pm2 monitor`, not a variant of it: monitor answers
// "what are my managed applications doing", dashboard answers "what is
// this machine doing, and which part of it is mine". Running it needs no
// daemon — without one the machine panel and the process list still work
// and only the task list is empty.
var Cmd = &cobra.Command{
	Use:   "dashboard",
	Short: "System activity monitor: host resources, process tree, and ports",
	Long: "Live whole-machine view: CPU, memory, network, disk I/O and\n" +
		"filesystem usage on top; pm2 tasks or every OS process below.\n" +
		"Selecting a row breaks it down into the sub-processes it spawned\n" +
		"and the ports its tree listens on.\n\n" +
		"Press `a` to switch between pm2 tasks and all processes, `s` to\n" +
		"cycle the sort order. For a periodic machine-readable feed of the\n" +
		"same data, use `pm2 dashboard emit`.",
	Args: cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		program := tea.NewProgram(
			dashboard.New(cliruntime.SocketPath()),
			tea.WithAltScreen(),
		)
		_, err := program.Run()
		return err
	},
}
