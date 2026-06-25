package cmd

import "github.com/spf13/cobra"

// ecoPlannerSystemPrefix is the prompt prefix injected into the
// process args when `wizard install --system-planner` is set.
const ecoPlannerSystemPrefix = "[plan only] run /system-planner and output to ./plans/"

// bindSystemPlannerFlag wires the --system-planner flag onto cmd.
// Lives in its own file so the two planner profiles can evolve
// independently (different help text, different validation rules,
// different args in the future) without touching eco_install.go.
func bindSystemPlannerFlag(cmd *cobra.Command, target *bool) {
	cmd.Flags().BoolVar(target, "system-planner", false,
		"wrap the process with the system-planner prompt prefix")
}
