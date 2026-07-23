package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bizshuk/pm2/config/wizard"
	"github.com/bizshuk/pm2/process"
	"github.com/spf13/cobra"
)

const (
	// ecoPlannerNS is the namespace assigned to processes installed via
	// `wizard install --system-planner` / `--business-planner`. The
	// prefix text itself is owned by per-planner files
	// (eco_install_system.go, eco_install_business.go).
	ecoPlannerNS = "planner"
)

var (
	installSystemPlanner   bool
	installBusinessPlanner bool
	installOutput          string
	installForce           bool
	installNoMerge         bool
)

// WizardInstallCmd is the `pm2 wizard install <script> [user_prompt]`
// subcommand. It registers a single pre-configured AppConfig and
// (currently) just writes the ecosystem file. Daemon RPC startup is
// left to the existing `pm2 start` flow so the install command stays
// synchronous and inspectable.
//
// The wizard shell (config/wizard) owns the merge-vs-replace decision
// and the rendering — this command only:
//
//   - wires the two planner flags + the standard write flags,
//   - assembles the AppConfig from a script + planner prefix +
//     optional user_prompt, and
//   - delegates the write step to wizard.RunInstall.
var WizardInstallCmd = &cobra.Command{
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
		if installSystemPlanner == installBusinessPlanner {
			return fmt.Errorf(
				"--system-planner and --business-planner are mutually exclusive; pass exactly one")
		}
		script := args[0]
		userPrompt := ""
		if len(args) >= 2 {
			userPrompt = args[1]
		}

		prefix := ecoPlannerSystemPrefix
		if installBusinessPlanner {
			prefix = ecoPlannerBusinessPrefix
		}

		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
		app := buildInstallApp(script, prefix, userPrompt, ecoPlannerNS, filepath.Base(cwd), cwd)

		out := cmd.OutOrStdout()
		errOut := cmd.ErrOrStderr()
		if err := wizard.RunInstall(
			wizard.WizardContext{
				In:     cmd.InOrStdin(),
				Out:    out,
				ErrOut: errOut,
			},
			app,
			wizard.InstallOptions{
				Output:  installOutput,
				Format:  wizard.FormatJS,
				Force:   installForce,
				NoMerge: installNoMerge,
			},
		); err != nil {
			return err
		}
		fmt.Fprintf(out, "Installed %s -> %s\n", app.Name, installOutput)
		fmt.Fprintf(out, "Next: pm2 start %s\n", installOutput)
		return nil
	},
}

func init() {
	bindSystemPlannerFlag(WizardInstallCmd, &installSystemPlanner)
	bindBusinessPlannerFlag(WizardInstallCmd, &installBusinessPlanner)
	WizardInstallCmd.Flags().StringVarP(&installOutput, "output", "o", "", "output file path (default: ./ecosystem.config.js)")
	WizardInstallCmd.Flags().BoolVarP(&installForce, "force", "f", false,
		"replace the entire output file instead of merging")
	WizardInstallCmd.Flags().BoolVar(&installNoMerge, "no-merge", false,
		"abort if the output file already exists (legacy behavior)")
}

// buildInstallApp assembles the AppConfig used by `wizard install`.
// The pm2-defined prefix and the optional user_prompt are joined into a
// SINGLE -p argument, wrapped in literal single quotes so the prompt
// survives as one token: ["-p", "'<prefix> <userPrompt>'"]. When the
// script is a known planner agent (agy/claude), "--add-dir <cwd>" is
// prepended so the agent has the workspace on its allow-list by default.
// The process name is derived as `<deriveName(script)>-<cwdBasename>`
// so multiple installs of the same script in different folders don't
// collide.
func buildInstallApp(script, prefix, userPrompt, namespace, cwdBasename, cwd string) process.AppConfig {
	name := wizard.DeriveName(script)
	if cwdBasename != "" {
		name = name + "-" + cwdBasename
	}

	prompt := prefix
	if userPrompt != "" {
		prompt = prefix + " " + userPrompt
	}
	var args []string
	if isPlannerAgent(script) {
		args = append(args, "--add-dir", cwd)
	}
	args = append(args, "-p", "'"+prompt+"'")

	a := process.AppConfig{
		Script:    script,
		Name:      name,
		Args:      args,
		Instances: 1,
		Namespace: namespace,
		Version:   wizard.DefaultVersion,
		CWD:       cwd,
		// A planner agent is a per-machine choice, not something every
		// consumer of this ecosystem file should be handed. Publishing it
		// as opt-in means `pm2 start owner/repo` skips it and prints the
		// `--with <name>` command instead.
		Optional: true,
	}
	a.Normalize("")
	return a
}

// isPlannerAgent reports whether the script is one of the AI planner
// agents (agy/claude) that should receive a default "--add-dir <cwd>"
// so the agent can read the current workspace without a prompt.
func isPlannerAgent(script string) bool {
	switch filepath.Base(script) {
	case "agy", "claude", "claudem", "claudew":
		return true
	}
	return false
}
