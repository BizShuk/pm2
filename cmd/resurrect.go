package cmd

import (
	"fmt"

	"github.com/bizshuk/pm2/model"
	"github.com/spf13/cobra"
)

// ResurrectCmd restores the process list from dump.json.
var ResurrectCmd = &cobra.Command{
	Use:   "resurrect",
	Short: "Restore previously saved process list",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := model.SendRequest(socketPath(), model.Request{Command: model.CmdResurrect})
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
