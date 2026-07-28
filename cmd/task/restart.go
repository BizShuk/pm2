package task

import (
	"fmt"

	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	"github.com/bizshuk/pm2/model"
	"github.com/spf13/cobra"
)

// RestartCmd restarts a managed task while preserving its configuration.
var RestartCmd = &cobra.Command{
	Use:   "restart <name|id|all>",
	Short: "Restart a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := model.SendRequest(cliruntime.SocketPath(), model.Request{
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
