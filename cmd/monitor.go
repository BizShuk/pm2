package cmd

import (
	"fmt"
	"strings"

	"github.com/bizshuk/pm2/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var monitorSortBy string

// MonitorCmd opens the live process dashboard.
var MonitorCmd = &cobra.Command{
	Use:     "monitor",
	Aliases: []string{"m", "dashboard"},
	Short:   "Live process detail and log dashboard",
	RunE: func(cmd *cobra.Command, args []string) error {
		s := tui.SortField(strings.ToLower(monitorSortBy))
		switch s {
		case tui.SortByName, tui.SortByNamespace, tui.SortByCPU, tui.SortByMem, tui.SortByStatus:
			// valid
		default:
			s = tui.SortByName
		}
		m := newMonitorModel(socketPath())
		m.SortBy = s
		p := tea.NewProgram(m, tea.WithAltScreen())
		finalModel, err := p.Run()
		if err == nil {
			if fm, ok := finalModel.(tui.Model); ok {
				fmt.Println(fm.View())
			}
		}
		return err
	},
}

func init() {
	MonitorCmd.Flags().StringVar(&monitorSortBy, "sort", "name", "sort processes by: name, namespace, cpu, memory, status")
}

func newMonitorModel(socket string) tui.Model {
	return tui.New(socket, true)
}
