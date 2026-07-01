package cmd

import (
	"fmt"

	"github.com/bizshuk/pm2/model"
	"github.com/spf13/cobra"
)

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <name|id|all>",
		Short: "Stop a process",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := model.SendRequest(socketPath(), model.Request{
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
		},
	}
}

func newRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart <name|id|all>",
		Short: "Restart a process",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := model.SendRequest(socketPath(), model.Request{
				Command: model.CmdRestart,
				Name:    args[0],
			})
			if err != nil {
				return err
			}
			if !resp.OK {
				return fmt.Errorf("%s", resp.Error)
			}
			fmt.Printf("restarted: %s\n", args[0])
			return nil
		},
	}
}

func newPauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause <name|id|all>",
		Short: "Suspend a process's cron schedule (status becomes 'paused')",
		Long: "Pause suspends a process: its cron schedule is removed so it will " +
			"not fire, and any running instance is stopped. The status shows " +
			"'paused' — distinct from 'stopped' — so a deliberately-held cron " +
			"task is not confused with one merely idle between fires. Resume with 'pm2 resume'.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := model.SendRequest(socketPath(), model.Request{
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
		},
	}
}

func newResumeCmd() *cobra.Command {
	return &cobra.Command{
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
}

func newDeleteCmd() *cobra.Command {
	return &cobra.Command{
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
}
