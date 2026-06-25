package cmd

import "github.com/spf13/cobra"

// ecoPlannerBusinessPrefix is the prompt prefix injected into the
// process args when `wizard install --business-planner` is set.
const ecoPlannerBusinessPrefix = "[plan only] run /business-planner and output to ./plans/"

// bindBusinessPlannerFlag wires the --business-planner flag onto cmd.
// Mirrors bindSystemPlannerFlag; kept in its own file so the two
// planner profiles can evolve independently.
func bindBusinessPlannerFlag(cmd *cobra.Command, target *bool) {
	cmd.Flags().BoolVar(target, "business-planner", false,
		"wrap the process with the business-planner prompt prefix")
}
