package daemon

import "github.com/spf13/cobra"

// RootStartCmd is the top-level short form of `pm2 daemon start`.
var RootStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the PM2 daemon (alias: pm2 daemon start)",
	Args:  cobra.NoArgs,
	RunE:  runStart,
}

func init() {
	bindStartFlags(RootStartCmd)
}
