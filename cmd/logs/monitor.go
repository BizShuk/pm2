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
// application log directory under ~/.config, daemon or no daemon.
var MonitorCmd = &cobra.Command{
	Use:     "monitor [app]",
	Aliases: []string{"m"},
	Short:   "Browse every application's logs under ~/.config",
	Long: "Browse and delete log files under ~/.config/<app>/logs.\n\n" +
		"The listing is taken from the filesystem, not from the daemon's " +
		"process list, so logs belonging to stopped, deleted, or never " +
		"registered tasks are all included.\n\n" +
		"[app] preselects and expands one application directory.",
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		target := ""
		if len(args) == 1 {
			target = args[0]
		}
		program := tea.NewProgram(
			logbrowser.New(cliruntime.ConfigRoot(), target),
			tea.WithAltScreen(),
		)
		_, err := program.Run()
		return err
	},
}
