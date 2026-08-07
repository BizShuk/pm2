package cmd

import (
	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	taskmanagercmd "github.com/bizshuk/pm2/cmd/taskmanager"
	"github.com/bizshuk/pm2/tui/dashboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// TaskmanagerCmd opens the system activity monitor.
//
// It is a peer of `pm2 monitor`, not a variant of it: monitor answers
// "what are my managed applications doing", taskmanager answers "what is
// this machine doing, and which part of it is mine". Running it needs no
// daemon — without one the machine panel and the process list still work
// and only the task list is empty.
//
// Subcommands live in cmd/taskmanager/:
//
//   - emit.go      — EmitCmd
//   - emit_text.go — text snapshot encoder
var TaskmanagerCmd = &cobra.Command{
	Use:     "taskmanager",
	Aliases: []string{"tm"},
	Short:   "System activity monitor: host resources, process tree, and ports (short alias: pm2 tm)",
	Long: "Live whole-machine view: CPU, memory, network, disk I/O and\n" +
		"filesystem usage on top; pm2 tasks or every OS process below.\n" +
		"Selecting a row breaks it down into the sub-processes it spawned\n" +
		"and the ports its tree listens on.\n\n" +
		"Press `a` to switch between pm2 tasks and all processes, `s` to\n" +
		"cycle the sort order. For a periodic machine-readable feed of the\n" +
		"same data, use `pm2 taskmanager emit`.",
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

func init() {
	TaskmanagerCmd.AddCommand(taskmanagercmd.EmitCmd)
}
