package wizard

import (
	"fmt"
	"os"
	"path/filepath"

	plannerprompt "github.com/bizshuk/pm2/cmd/wizard/prompt"
	corewizard "github.com/bizshuk/pm2/config/wizard"
	"github.com/bizshuk/pm2/process"
	"github.com/spf13/cobra"
)

const (
	// EcoPlannerNS is the namespace assigned to processes installed via
	// `wizard install --system-planner` / `--business-planner`. The
	// prompt text is owned by cmd/wizard/prompt.
	EcoPlannerNS = "planner"
)

var (
	installSystemPlanner   bool
	installBusinessPlanner bool
	installOutput          string
	installForce           bool
	installNoMerge         bool
)

// InstallCmd is the `pm2 wizard install <script> [user_prompt]`
// subcommand. It registers a single pre-configured AppConfig and
// (currently) just writes the ecosystem file. Daemon RPC startup is
// left to the existing `pm2 task start` flow so the install command stays
// synchronous and inspectable.
//
// The wizard shell (config/wizard) owns the merge-vs-replace decision
// and the rendering — this command only:
//
//   - wires the two planner flags + the standard write flags,
//   - assembles the AppConfig from a script + planner prefix +
//     optional user_prompt, and
//   - delegates the write step to wizard.RunInstall.
var InstallCmd = &cobra.Command{
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

		template := plannerprompt.System()
		if installBusinessPlanner {
			template = plannerprompt.Business()
		}

		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
		app := buildInstallApp(
			script,
			template.Render(userPrompt),
			EcoPlannerNS,
			filepath.Base(cwd),
			cwd,
		)

		out := cmd.OutOrStdout()
		errOut := cmd.ErrOrStderr()
		if err := corewizard.RunInstall(
			corewizard.WizardContext{
				In:     cmd.InOrStdin(),
				Out:    out,
				ErrOut: errOut,
			},
			app,
			corewizard.WriteOptions{
				Output:  installOutput,
				Format:  corewizard.FormatJS,
				Force:   installForce,
				NoMerge: installNoMerge,
			},
		); err != nil {
			return err
		}
		fmt.Fprintf(out, "Installed %s -> %s\n", app.Name, installOutput)
		fmt.Fprintf(out, "Next: pm2 task start %s\n", installOutput)
		return nil
	},
}

func init() {
	bindPlannerFlag(InstallCmd, plannerprompt.System(), &installSystemPlanner)
	bindPlannerFlag(InstallCmd, plannerprompt.Business(), &installBusinessPlanner)
	InstallCmd.Flags().StringVarP(&installOutput, "output", "o", "", "output file path (default: ./ecosystem.config.js)")
	InstallCmd.Flags().BoolVarP(&installForce, "force", "f", false,
		"replace the entire output file instead of merging")
	InstallCmd.Flags().BoolVar(&installNoMerge, "no-merge", false,
		"abort if the output file already exists (legacy behavior)")
}

// buildInstallApp assembles the AppConfig used by `wizard install`.
// The rendered planner prompt is wrapped in literal single quotes so it
// survives as one token: ["-p", "'<prompt>'"]. When the
// script is a known planner agent (agy/claude), "--add-dir <cwd>" is
// prepended so the agent has the workspace on its allow-list by default.
// The process name is derived as `<deriveName(script)>-<cwdBasename>`
// so multiple installs of the same script in different folders don't
// collide.
func buildInstallApp(script, renderedPrompt, namespace, cwdBasename, cwd string) process.AppConfig {
	name := corewizard.DeriveName(script)
	if cwdBasename != "" {
		name = name + "-" + cwdBasename
	}

	var args []string
	if isPlannerAgent(script) {
		args = append(args, "--add-dir", cwd)
	}
	args = append(args, "-p", "'"+renderedPrompt+"'")

	a := process.AppConfig{
		Script:    script,
		Name:      name,
		Args:      args,
		Instances: process.DefaultInstances,
		Namespace: namespace,
		Version:   corewizard.DefaultVersion,
		CWD:       cwd,
		// A planner agent is a per-machine choice, not something every
		// consumer of this ecosystem file should be handed. Publishing it
		// as opt-in means `pm2 task start owner/repo` registers it paused and
		// prints the resume command instead.
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
