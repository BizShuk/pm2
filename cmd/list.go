package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/olekukonko/tablewriter"
	"github.com/shuk/pm2/daemon"
	"github.com/shuk/pm2/process"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"l", "ls", "status"},
		Short:   "List all managed processes",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := daemon.SendRequest(socketPath(), daemon.Request{
				Command: daemon.CmdList,
			})
			if err != nil {
				fmt.Println("No processes running (daemon not started).")
				return nil
			}
			if !resp.OK {
				return fmt.Errorf("%s", resp.Error)
			}

			var infos []process.ProcessInfo
			if err := json.Unmarshal(resp.Payload, &infos); err != nil {
				return fmt.Errorf("parse list: %w", err)
			}

			sort.Slice(infos, func(i, j int) bool {
				return infos[i].ID < infos[j].ID
			})

			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"ID", "Namespace", "Name", "PID", "Status", "Restarts", "Cron"})
			table.SetBorder(false)
			table.SetColumnSeparator("│")

			for _, p := range infos {
				pid := fmt.Sprintf("%d", p.PID)
				if p.PID == 0 {
					pid = "-"
				}
				table.Append([]string{
					fmt.Sprintf("%d", p.ID),
					p.Namespace,
					p.Name,
					pid,
					string(p.Status),
					fmt.Sprintf("%d", p.Restarts),
					p.CronRestart,
				})
			}
			table.Render()
			return nil
		},
	}
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
