package cmd

import (
	"fmt"

	"github.com/bizshuk/pm2/model"
	"github.com/spf13/cobra"
)

// ResumeCmd resumes a paused process and restores its cron schedule.
var ResumeCmd = &cobra.Command{
	Use:     "resume <name|id|all>",
	Aliases: []string{"unpause"},
	Short:   "Resume a paused process (re-registers its cron schedule)",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := model.SendRequest(socketPath(), model.Request{
			Command: model.CmdResume,
			Name:    args[0],
		})
		if err != nil {
			return err
		}
		if !resp.OK {
			return fmt.Errorf("%s", resp.Error)
		}
		fmt.Printf("resumed: %s\n", args[0])
		return nil
	},
}
