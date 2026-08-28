package cmd

import (
	"time"

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
		"cycle the sort order. Each refresh re-ranks the list, so the\n" +
		"cursor follows the row it is on rather than its position, and\n" +
		"`--interval` sets how often that re-ranking happens.\n\n" +
		"For a periodic machine-readable feed of the same data, use\n" +
		"`pm2 taskmanager emit`.",
	Args: cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		model := dashboard.New(cliruntime.SocketPath())
		if taskmanagerInterval >= dashboard.MinInterval {
			model.Interval = taskmanagerInterval
		}
		program := tea.NewProgram(
			model,
			tea.WithAltScreen(),
		)
		_, err := program.Run()
		return err
	},
}

// taskmanagerInterval is how long the view sits still between
// collections. A value below dashboard.MinInterval is ignored rather
// than rejected: a darwin sample blocks for about a second, so asking
// for less only queues passes behind each other.
var taskmanagerInterval time.Duration

func init() {
	TaskmanagerCmd.Flags().DurationVar(&taskmanagerInterval, "interval", dashboard.DefaultInterval,
		"how often to re-sample the machine and re-rank the list (minimum 1s)")
	TaskmanagerCmd.AddCommand(taskmanagercmd.EmitCmd)
}
