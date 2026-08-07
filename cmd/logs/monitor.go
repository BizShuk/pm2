// Package logs owns subcommands of `pm2 logs`. The parent command lives
// in package cmd (cmd/logs.go).
package logs

import (
	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	"github.com/bizshuk/pm2/tui/logbrowser"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// MonitorCmd opens the interactive managed-log Tree Explorer and Viewer.
var MonitorCmd = &cobra.Command{
	Use:     "monitor [name]",
	Aliases: []string{"m"},
	Short:   "Interactive log Tree Explorer and Viewer",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		target := ""
		if len(args) == 1 {
			target = args[0]
		}
		program := tea.NewProgram(
			logbrowser.New(cliruntime.SocketPath(), target),
			tea.WithAltScreen(),
		)
		_, err := program.Run()
		return err
	},
}
