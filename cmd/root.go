package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bizshuk/gosdk/metric"
	"github.com/spf13/cobra"
)

var pm2Home string

var rootCmd = &cobra.Command{
	Use:   "pm2",
	Short: "PM2-like process manager written in Go",
}

func Execute() {
	if len(os.Args) > 1 {
		arg := os.Args[1]
		if arg == "version" || arg == "-v" || arg == "--v" || arg == "--version" || arg == "-version" {
			fmt.Println("1.0.0")
			os.Exit(0)
		}
	}
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot determine home dir:", err)
		os.Exit(1)
	}
	pm2Home = filepath.Join(home, ".pm2")
	_ = os.MkdirAll(pm2Home, 0755)

	rootCmd.AddCommand(
		newStartCmd(),
		newStopCmd(),
		newRestartCmd(),
		newDeleteCmd(),
		newLogsCmd(),
		newSaveCmd(),
		newResurrectCmd(),
		newStartupCmd(),
		newDaemonCmd(),
		newKillCmd(),
		newMonitCmd(),
		newEcoCmd(),
	)

	// EnableTraverseRunHooks ensures the root PersistentPreRunE fires for
	// every subcommand, even those that define their own PersistentPreRunE.
	cobra.EnableTraverseRunHooks = true
	metric.CobraCMDHook(rootCmd)
}

func socketPath() string {
	return filepath.Join(pm2Home, "pm2.sock")
}
