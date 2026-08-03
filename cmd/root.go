// Package cmd composes the complete PM2 Cobra command tree.
package cmd

import (
	sdkcmd "github.com/bizshuk/gosdk/cmd"
	sdkconfig "github.com/bizshuk/gosdk/config"
	"github.com/bizshuk/gosdk/metric"
	daemoncmd "github.com/bizshuk/pm2/cmd/daemon"
	dashboardcmd "github.com/bizshuk/pm2/cmd/dashboard"
	taskcmd "github.com/bizshuk/pm2/cmd/task"
	wizardcmd "github.com/bizshuk/pm2/cmd/wizard"
	"github.com/spf13/cobra"
)

// Cmd is the customized PM2 root command.
var Cmd = &cobra.Command{
	Use:   "pm2",
	Short: "PM2-like process manager written in Go",
}

func init() {
	sdkconfig.Default(sdkconfig.WithAppName("pm2"))

	Cmd.AddCommand(
		daemoncmd.RootStartCmd,
		taskcmd.ApplyCmd,
		ListCmd,
		LogsCmd,
		SaveCmd,
		ResurrectCmd,
		StartupCmd,
		daemoncmd.Cmd,
		taskcmd.Cmd,
		MonitorCmd,
		dashboardcmd.Cmd,
		wizardcmd.Cmd,
		sdkcmd.ConfigCmd,
	)

	// EnableTraverseRunHooks ensures the root PersistentPreRunE fires for
	// every subcommand, even those that define their own PersistentPreRunE.
	cobra.EnableTraverseRunHooks = true
	metric.CobraCMDHook(Cmd)
}
