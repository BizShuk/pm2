package cmd

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/shuk/pm2/tui"
	"github.com/spf13/cobra"
)

func newMonitCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "monit",
		Aliases: []string{"monitor", "dashboard"},
		Short:   "Interactive live process dashboard (TUI)",
		RunE: func(cmd *cobra.Command, args []string) error {
			m := tui.New(socketPath())
			p := tea.NewProgram(m, tea.WithAltScreen())
			_, err := p.Run()
			return err
		},
	}
}
