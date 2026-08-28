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
//
// SilenceUsage keeps a failed run to a single error line. The daemon is
// spawned with its stderr redirected to an append-only file no rotation
// owns, so a respawn loop against an argv the binary rejects writes the
// full usage block on every attempt — that is how the daemon-err.log
// reached 135 MB of repeated `unknown flag: --foreground` help text. A
// usage block is for a human at a terminal, not for a supervisor's log.
var Cmd = &cobra.Command{
	Use:          "pm2",
	Short:        "PM2-like process manager written in Go",
	SilenceUsage: true,
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
		GpuCmd,
		TaskCmd,
		WorkflowCmd,
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
