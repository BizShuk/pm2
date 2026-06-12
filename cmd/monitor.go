package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shuk/pm2/daemon"
	"github.com/shuk/pm2/tui"
	"github.com/spf13/cobra"
)

func newMonitCmd() *cobra.Command {
	var detail bool
	cmd := &cobra.Command{
		Use:     "monit",
		Aliases: []string{"m", "monitor", "dashboard"},
		Short:   "Live process dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			m := tui.New(socketPath(), detail)
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
	cmd.Flags().BoolVarP(&detail, "detail", "d", false, "show process details and logs")
	return cmd
}

func newSaveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "save",
		Short: "Persist current process list to dump.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := daemon.SendRequest(socketPath(), daemon.Request{Command: daemon.CmdSave})
			if err != nil {
				return err
			}
			if !resp.OK {
				return fmt.Errorf("%s", resp.Error)
			}
			fmt.Println("Process list saved.")
			return nil
		},
	}
}

func newResurrectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resurrect",
		Short: "Restore previously saved process list",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := daemon.SendRequest(socketPath(), daemon.Request{Command: daemon.CmdResurrect})
			if err != nil {
				return err
			}
			if !resp.OK {
				return fmt.Errorf("%s", resp.Error)
			}
			fmt.Println("Processes resurrected.")
			return nil
		},
	}
}
