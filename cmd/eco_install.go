package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bizshuk/pm2/config"
	"github.com/spf13/cobra"
)

const (
	// ecoPlannerNS is the namespace assigned to processes installed via
	// `wizard install --system-planner` / `--business-planner`. The
	// prefix text itself is owned by per-planner files
	// (eco_install_system.go, eco_install_business.go).
	ecoPlannerNS = "planner"
)

// newEcoInstallCmd builds the `pm2 wizard install <script> [user_prompt]`
// subcommand. It registers a single pre-configured AppConfig and
// (currently) just writes the ecosystem file. Daemon RPC startup is
// left to the existing `pm2 start` flow so the install command stays
// synchronous and inspectable.
func newEcoInstallCmd() *cobra.Command {
	var (
		systemPlanner   bool
		businessPlanner bool
		output          string
		force           bool
		noMerge         bool
	)
	cmd := &cobra.Command{
		Use:   "install <script> [user_prompt]",
		Short: "Register a pre-configured process in ecosystem.config.js",
		Long: "Writes a single AppConfig built from the given script and a " +
			"pm2-defined prompt prefix. Pass exactly one of --system-planner " +
			"or --business-planner to choose the prefix; the optional " +
			"user_prompt is appended as the third args element. The resulting " +
			"AppConfig is merged into the existing ecosystem file (or written " +
			"fresh if none exists). The process namespace is set to `planner` " +
			"and the process name is `<script>-<current-folder>`.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if systemPlanner == businessPlanner {
				return fmt.Errorf(
					"--system-planner and --business-planner are mutually exclusive; pass exactly one")
			}
			script := args[0]
			userPrompt := ""
			if len(args) >= 2 {
				userPrompt = args[1]
			}

			prefix := ecoPlannerSystemPrefix
			if businessPlanner {
				prefix = ecoPlannerBusinessPrefix
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getwd: %w", err)
			}
			app := buildInstallApp(script, prefix, userPrompt, ecoPlannerNS, filepath.Base(cwd), cwd)

			out := cmd.OutOrStdout()
			errOut := cmd.ErrOrStderr()
			if output == "" {
				output = ecoDefaultOutput
			}
			if err := writeEcosystemFile(
				[]config.AppConfig{app}, output, force, noMerge,
				ecoFormatJS, cmd.InOrStdin(), out, errOut, true /* yesAll */); err != nil {
				return err
			}
			fmt.Fprintf(out, "Installed %s -> %s\n", app.Name, output)
			fmt.Fprintf(out, "Next: pm2 start %s\n", output)
			return nil
		},
	}
	bindSystemPlannerFlag(cmd, &systemPlanner)
	bindBusinessPlannerFlag(cmd, &businessPlanner)
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file path (default: ./ecosystem.config.js)")
	cmd.Flags().BoolVarP(&force, "force", "f", false,
		"replace the entire output file instead of merging")
	cmd.Flags().BoolVar(&noMerge, "no-merge", false,
		"abort if the output file already exists (legacy behavior)")
	return cmd
}

// buildInstallApp assembles the AppConfig used by `wizard install`.
// Args is always the 3-element array ["-p", prefix, userPrompt] so
// the downstream process can index args[1] / args[2] without
// checking length; userPrompt is the empty string when the user
// omitted it. The process name is derived as
// `<deriveName(script)>-<cwdBasename>` so multiple installs of the
// same script in different folders don't collide.
func buildInstallApp(script, prefix, userPrompt, namespace, cwdBasename, cwd string) config.AppConfig {
	name := deriveName(script)
	if cwdBasename != "" {
		name = name + "-" + cwdBasename
	}
	a := config.AppConfig{
		Script:    script,
		Name:      name,
		Args:      []string{"-p", prefix, userPrompt},
		Instances: 1,
		Namespace: namespace,
		Version:   ecoDefaultVersion,
		CWD:       cwd,
	}
	a.Normalize()
	return a
}
