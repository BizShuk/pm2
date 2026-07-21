package cmd

import (
	"fmt"
	"strings"

	"github.com/bizshuk/pm2/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var monitSortBy string

// MonitCmd opens the live process dashboard.
var MonitCmd = &cobra.Command{
	Use:     "monit",
	Aliases: []string{"m", "monitor", "dashboard"},
	Short:   "Live process detail and log dashboard",
	RunE: func(cmd *cobra.Command, args []string) error {
		s := tui.SortField(strings.ToLower(monitSortBy))
		switch s {
		case tui.SortByName, tui.SortByNamespace, tui.SortByCPU, tui.SortByMem, tui.SortByStatus:
			// valid
		default:
			s = tui.SortByName
		}
		m := newMonitModel(socketPath())
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
	MonitCmd.Flags().StringVar(&monitSortBy, "sort", "name", "sort processes by: name, namespace, cpu, memory, status")
}

func newMonitModel(socket string) tui.Model {
	return tui.New(socket, true)
}
