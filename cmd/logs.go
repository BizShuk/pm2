package cmd

import (
	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	"github.com/bizshuk/pm2/tui/logbrowser"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// LogsCmd opens the interactive application → file → log viewer.
var LogsCmd = &cobra.Command{
	Use:   "logs [name]",
	Short: "Interactive log file browser and manager",
	Args:  cobra.MaximumNArgs(1),
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
