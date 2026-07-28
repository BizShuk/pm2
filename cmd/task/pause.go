package task

import (
	"fmt"

	appcmd "github.com/bizshuk/pm2/cmd"
	"github.com/bizshuk/pm2/model"
	"github.com/spf13/cobra"
)

// PauseCmd suspends a process and removes its active cron schedule.
var PauseCmd = &cobra.Command{
	Use:   "pause <name|id|all>",
	Short: "Pause a task and its cron schedule",
	Long: "Pause suspends a process: its cron schedule is removed so it will " +
		"not fire, and any running instance is stopped. The status shows " +
		"'paused' — distinct from 'stopped' — so a deliberately-held cron " +
		"task is not confused with one merely idle between fires. Resume with 'pm2 task resume'.",
	Args: cobra.ExactArgs(1),
	RunE: runPause,
}

func runPause(_ *cobra.Command, args []string) error {
	resp, err := model.SendRequest(appcmd.SocketPath(), model.Request{
		Command: model.CmdPause,
		Name:    args[0],
	})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	fmt.Printf("paused: %s\n", args[0])
	return nil
}
