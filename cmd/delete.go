package cmd

import (
	"fmt"

	"github.com/bizshuk/pm2/model"
	"github.com/spf13/cobra"
)

// DeleteCmd stops and removes a managed process from the daemon registry.
var DeleteCmd = &cobra.Command{
	Use:     "delete <name|id|all>",
	Aliases: []string{"del"},
	Short:   "Remove a process from the list",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := model.SendRequest(socketPath(), model.Request{
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
	},
}
