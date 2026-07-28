package task

import (
	"fmt"

	appcmd "github.com/bizshuk/pm2/cmd"
	"github.com/bizshuk/pm2/model"
	"github.com/spf13/cobra"
)

// DeleteCmd stops and removes a managed process from the daemon registry.
var DeleteCmd = &cobra.Command{
	Use:   "delete <name|id|all>",
	Short: "Delete a task from the process list",
	Args:  cobra.ExactArgs(1),
	RunE:  runDelete,
}

func runDelete(_ *cobra.Command, args []string) error {
	resp, err := model.SendRequest(appcmd.SocketPath(), model.Request{
		Command: model.CmdDelete,
		Name:    args[0],
	})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	fmt.Printf("deleted: %s\n", args[0])
	return nil
}
