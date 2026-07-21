package main

import (
	"fmt"
	"os"

	sdkcmd "github.com/bizshuk/gosdk/cmd"
	sdkconfig "github.com/bizshuk/gosdk/config"
	"github.com/bizshuk/gosdk/metric"
	appcmd "github.com/bizshuk/pm2/cmd"
	"github.com/bizshuk/pm2/model"
	"github.com/spf13/cobra"
)

// RootCmd is the composition root for the pm2 CLI.
var RootCmd = &cobra.Command{
	Use:   "pm2",
	Short: "PM2-like process manager written in Go",
}

func init() {
	sdkconfig.Default(sdkconfig.WithAppName("pm2"))

	RootCmd.AddCommand(
		appcmd.StartCmd,
		appcmd.StopCmd,
		appcmd.RestartCmd,
		appcmd.PauseCmd,
		appcmd.ResumeCmd,
		appcmd.DeleteCmd,
		appcmd.ListCmd,
		appcmd.LogsCmd,
		appcmd.SaveCmd,
		appcmd.ResurrectCmd,
		appcmd.StartupCmd,
		appcmd.DaemonCmd,
		appcmd.MonitorCmd,
		appcmd.WizardCmd,
		sdkcmd.ConfigCmd,
	)

	// EnableTraverseRunHooks ensures the root PersistentPreRunE fires for
	// every subcommand, even those that define their own PersistentPreRunE.
	cobra.EnableTraverseRunHooks = true
	metric.CobraCMDHook(RootCmd)
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--v", "--version", "-version":
			fmt.Println(model.PM2Version)
			return
		}
	}
	if err := RootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
