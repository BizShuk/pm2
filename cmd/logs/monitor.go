// Package logs owns subcommands of `pm2 logs`. The parent command lives
// in package cmd (cmd/logs.go).
package logs

import (
	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	"github.com/bizshuk/pm2/tui/logbrowser"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// MonitorCmd opens the interactive log Tree Explorer and Viewer over every
// managed task's logs, daemon or no daemon.
var MonitorCmd = &cobra.Command{
	Use:     "monitor [task]",
	Aliases: []string{"m"},
	Short:   "Browse every task's logs under ~/.config/pm2/tasks/logs",
	Long: "Browse and delete log files under ~/.config/pm2/tasks/logs.\n\n" +
		"The listing is taken from the filesystem, not from the daemon's " +
		"process list, so logs belonging to stopped, deleted, or never " +
		"registered tasks are all included.\n\n" +
		"[task] preselects and expands one task.",
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		target := ""
		if len(args) == 1 {
			target = args[0]
		}
		program := tea.NewProgram(
			logbrowser.New(cliruntime.TaskLogsDir(), target),
			tea.WithAltScreen(),
		)
		_, err := program.Run()
		return err
	},
}
