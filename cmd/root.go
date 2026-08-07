// Package cmd composes the complete PM2 Cobra command tree.
//
// Layout convention:
//
//   - First-layer commands are files in this package (cmd/<command>.go).
//   - A first-layer command's subcommands live in cmd/<command>/<subcommand>.go.
package cmd

import (
	sdkcmd "github.com/bizshuk/gosdk/cmd"
	sdkconfig "github.com/bizshuk/gosdk/config"
	"github.com/bizshuk/gosdk/metric"
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
		StartCmd,
		ApplyCmd,
		ListCmd,
		LogsCmd,
		SaveCmd,
		ResurrectCmd,
		StartupCmd,
		DaemonCmd,
		TaskCmd,
		MonitorCmd,
		TaskmanagerCmd,
		WizardCmd,
		sdkcmd.ConfigCmd,
	)

	// EnableTraverseRunHooks ensures the root PersistentPreRunE fires for
	// every subcommand, even those that define their own PersistentPreRunE.
	cobra.EnableTraverseRunHooks = true
	metric.CobraCMDHook(Cmd)
}
