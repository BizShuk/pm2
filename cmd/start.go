package cmd

import (
	daemoncmd "github.com/bizshuk/pm2/cmd/daemon"
	"github.com/spf13/cobra"
)

// StartCmd is the top-level short form of `pm2 daemon start`.
var StartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the PM2 daemon (alias: pm2 daemon start)",
	Args:  cobra.NoArgs,
	RunE:  daemoncmd.RunStart,
}

func init() {
	daemoncmd.BindStartFlags(StartCmd)
}
