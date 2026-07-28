package cmd

import (
	"fmt"

	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	"github.com/bizshuk/pm2/model"
	"github.com/spf13/cobra"
)

// SaveCmd persists the current process list to dump.json.
var SaveCmd = &cobra.Command{
	Use:     "save",
	Aliases: []string{"s"},
	Short:   "Persist current process list to dump.json (short alias: pm2 s)",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := model.SendRequest(cliruntime.SocketPath(), model.Request{Command: model.CmdSave})
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
