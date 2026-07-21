package cmd

import (
	"fmt"

	"github.com/bizshuk/pm2/model"
	"github.com/spf13/cobra"
)

// SaveCmd persists the current process list to dump.json.
var SaveCmd = &cobra.Command{
	Use:   "save",
	Short: "Persist current process list to dump.json",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := model.SendRequest(socketPath(), model.Request{Command: model.CmdSave})
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
