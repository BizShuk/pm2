package task

import (
	"fmt"

	appcmd "github.com/bizshuk/pm2/cmd"
	"github.com/bizshuk/pm2/model"
	"github.com/spf13/cobra"
)

// ResumeCmd resumes a paused process and restores its cron schedule.
var ResumeCmd = &cobra.Command{
	Use:   "resume <name|id|all>",
	Short: "Resume a paused task and cron schedule",
	Args:  cobra.ExactArgs(1),
	RunE:  runResume,
}

func runResume(_ *cobra.Command, args []string) error {
	resp, err := model.SendRequest(appcmd.SocketPath(), model.Request{
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
}
