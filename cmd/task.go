package cmd

import (
	"fmt"

	taskcmd "github.com/bizshuk/pm2/cmd/task"
	"github.com/spf13/cobra"
)

// TaskCmd groups task lifecycle commands under `pm2 task`.
//
// Subcommands live in cmd/task/:
//
//   - start.go   — StartCmd + ecosystem load / AppStartReq flow
//   - restart.go — RestartCmd
//   - stop.go    — StopCmd
//   - pause.go   — PauseCmd
//   - resume.go  — ResumeCmd
//   - delete.go  — DeleteCmd
var TaskCmd = &cobra.Command{
	Use:     "task",
	Aliases: []string{"t"},
	Short:   "Manage task lifecycle commands (short alias: pm2 t)",
	Args:    cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		return fmt.Errorf("pm2 task requires a subcommand (start | restart | stop | pause | resume | delete)")
	},
}

func init() {
	TaskCmd.AddCommand(
		taskcmd.StartCmd,
		taskcmd.RestartCmd,
		taskcmd.StopCmd,
		taskcmd.PauseCmd,
		taskcmd.ResumeCmd,
		taskcmd.DeleteCmd,
	)
}
