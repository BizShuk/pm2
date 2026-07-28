package task

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Cmd groups task lifecycle commands under `pm2 task`.
var Cmd = &cobra.Command{
	Use:     "task",
	Aliases: []string{"t"},
	Short:   "Manage task lifecycle commands (short alias: pm2 t)",
	Args:    cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		return fmt.Errorf("pm2 task requires a subcommand (start | restart | stop | pause | resume | delete)")
	},
}

func init() {
	Cmd.AddCommand(StartCmd, RestartCmd, StopCmd, PauseCmd, ResumeCmd, DeleteCmd)
}
