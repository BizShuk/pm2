package cmd

import (
	"os"
	"os/signal"
	"syscall"

	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	"github.com/spf13/cobra"
)

// LogsCmd streams managed application logs to stdout and stderr.
var LogsCmd = &cobra.Command{
	Use:   "logs [name]",
	Short: "Stream managed application logs to stdout and stderr",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := ""
		if len(args) == 1 {
			target = args[0]
		}
		ctx, stop := signal.NotifyContext(
			cmd.Context(),
			os.Interrupt,
			syscall.SIGTERM,
		)
		defer stop()
		return streamLogs(
			ctx,
			cliruntime.SocketPath(),
			target,
			cmd.OutOrStdout(),
			cmd.ErrOrStderr(),
		)
	},
}
