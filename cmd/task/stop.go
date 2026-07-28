package task

import (
	"fmt"

	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	"github.com/bizshuk/pm2/model"
	"github.com/spf13/cobra"
)

// StopCmd stops a managed process without stopping the daemon.
var StopCmd = &cobra.Command{
	Use:   "stop <name|id|all>",
	Short: "Stop a task",
	Args:  cobra.ExactArgs(1),
	RunE:  runStop,
}

func runStop(_ *cobra.Command, args []string) error {
	resp, err := model.SendRequest(cliruntime.SocketPath(), model.Request{
		Command: model.CmdStop,
		Name:    args[0],
	})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	fmt.Printf("stopped: %s\n", args[0])
	return nil
}
