package wizard

import (
	plannerprompt "github.com/bizshuk/pm2/cmd/wizard/prompt"
	"github.com/spf13/cobra"
)

func bindPlannerFlag(cmd *cobra.Command, template plannerprompt.Template, target *bool) {
	cmd.Flags().BoolVar(target, template.Flag, false, template.Help)
}
