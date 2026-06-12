package cmd

import (
	"fmt"

	"github.com/shuk/pm2/daemon"
	"github.com/spf13/cobra"
)

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <name|id|all>",
		Short: "Stop a process",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := daemon.SendRequest(socketPath(), daemon.Request{
				Command: daemon.CmdStop,
				Name:    args[0],
			})
			if err != nil {
				return err
			}
			if !resp.OK {
				return fmt.Errorf(resp.Error)
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
			resp, err := daemon.SendRequest(socketPath(), daemon.Request{
				Command: daemon.CmdRestart,
				Name:    args[0],
			})
			if err != nil {
				return err
			}
			if !resp.OK {
				return fmt.Errorf(resp.Error)
			}
			fmt.Printf("restarted: %s\n", args[0])
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
			resp, err := daemon.SendRequest(socketPath(), daemon.Request{
				Command: daemon.CmdDelete,
				Name:    args[0],
			})
			if err != nil {
				return err
			}
			if !resp.OK {
				return fmt.Errorf(resp.Error)
			}
			fmt.Printf("deleted: %s\n", args[0])
			return nil
		},
	}
}
