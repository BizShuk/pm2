package cmd

import (
	"fmt"

	"github.com/bizshuk/pm2/model"
	"github.com/spf13/cobra"
)

// RestartCmd restarts a managed process while preserving its configuration.
var RestartCmd = &cobra.Command{
	Use:   "restart <name|id|all>",
	Short: "Restart a process",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := model.SendRequest(socketPath(), model.Request{
			Command: model.CmdRestart,
			Name:    args[0],
		})
		if err != nil {
			return err
		}
		if !resp.OK {
			return fmt.Errorf("%s", resp.Error)
		}
		fmt.Printf("restarted: %s\n", args[0])
		return nil
	},
}
