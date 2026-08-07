package cmd

import (
	taskcmd "github.com/bizshuk/pm2/cmd/task"
	"github.com/spf13/cobra"
)

// ApplyCmd is the explicit root-level short alias for `pm2 task start`.
var ApplyCmd = &cobra.Command{
	Use:   "apply [ecosystem.config.js|ecosystem.config.json|owner/repo|https://github.com/...]",
	Short: "Apply an ecosystem config (alias: pm2 task start)",
	Long: "Apply tasks from an ecosystem config file or remote GitHub repository. " +
		"With no target, loads ecosystem.config.js from the current directory.",
	Args: cobra.MaximumNArgs(1),
	RunE: taskcmd.RunTasks,
	Example: `  pm2 apply
  pm2 apply --single
  pm2 task start ecosystem.config.js  # canonical command`,
}

func init() {
	taskcmd.BindStartFlags(ApplyCmd)
}
