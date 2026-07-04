package cmd

import (
	"fmt"
	"os"

	"github.com/bizshuk/pm2/config/wizard"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// isTerminalFunc is the terminal-detection function used by the wizard.
// Overridden in tests to bypass TTY detection when piping stdin from a
// strings.Reader.
var isTerminalFunc = isatty.IsTerminal

// interactiveFlags are shared by the interactive wizard (currently the
// top-level `pm2 wizard` command). Hoisted so subcommands and helpers
// can read them without re-binding.
type interactiveFlags struct {
	output  string
	force   bool
	format  string
	yesAll  bool
	noMerge bool
}

// newEcoCmd returns the `pm2 wizard` command. It only wires Cobra
// flags + I/O streams and delegates every behavioural step to
// config/wizard (see plans/architecture-wizard-decoupling.md).
func newEcoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "wizard",
		Aliases: []string{"w"},
		Short:   "Interactively build an ecosystem.config.js (or .json)",
		Long: "Walks through a series of questions and writes a valid ecosystem.config.js " +
			"in the current directory that `pm2 start` can load directly. " +
			"If the output file already exists, wizard merges the new apps into it " +
			"by default; pass --force to replace, or --no-merge to abort.",
		Args: cobra.NoArgs,
		RunE: runEcoInteractive,
	}
	flags := defaultInteractiveFlags()
	bindInteractiveFlags(cmd, &flags)
	cmd.AddCommand(newEcoInstallCmd())
	return cmd
}

func defaultInteractiveFlags() interactiveFlags {
	return interactiveFlags{format: wizard.FormatJS}
}

func bindInteractiveFlags(cmd *cobra.Command, f *interactiveFlags) {
	cmd.Flags().StringVarP(&f.output, "output", "o", "", "output file path (default: ./ecosystem.config.js)")
	cmd.Flags().BoolVarP(&f.force, "force", "f", false,
		"replace the entire output file with the newly collected apps, "+
			"bypassing the merge and bypassing parse errors on the existing file")
	cmd.Flags().StringVar(&f.format, "format", "js",
		"output format when creating a new file: js|json "+
			"(existing file's extension wins on merge)")
	cmd.Flags().BoolVarP(&f.yesAll, "yes", "y", false, "accept all defaults (non-interactive)")
	cmd.Flags().BoolVar(&f.noMerge, "no-merge", false,
		"if the output file exists, abort instead of merging (legacy behavior). "+
			"Combine with --force to replace.")
}

func runEcoInteractive(cmd *cobra.Command, _ []string) error {
	flags := defaultInteractiveFlags()
	if v, err := cmd.Flags().GetString("output"); err == nil {
		flags.output = v
	}
	if v, err := cmd.Flags().GetBool("force"); err == nil {
		flags.force = v
	}
	if v, err := cmd.Flags().GetString("format"); err == nil {
		flags.format = v
	}
	if v, err := cmd.Flags().GetBool("yes"); err == nil {
		flags.yesAll = v
	}
	if v, err := cmd.Flags().GetBool("no-merge"); err == nil {
		flags.noMerge = v
	}
	return runInteractive(cmd, &flags)
}

// runInteractive is the thin Cobra shell around wizard.RunInteractive.
// Two responsibilities that DO NOT belong in the wizard package:
//
//  1. TTY gate: refuse to read prompts from a non-terminal stdin
//     unless --yes is set. The wizard never touches os.Stdin
//     directly so it cannot tell — we pass the verdict in via
//     WizardContext.YesAll.
//  2. Stream binding: pull cobra's In/Out/Err into a WizardContext
//     so the wizard never reaches into cobra or os types.
func runInteractive(cmd *cobra.Command, flags *interactiveFlags) error {
	tty := isTerminalFunc(os.Stdin.Fd())
	if !tty && !flags.yesAll {
		fmt.Fprintln(cmd.ErrOrStderr(),
			"pm2 eco requires an interactive terminal. "+
				"Re-run with --yes to generate a config with all defaults.")
		return fmt.Errorf("non-interactive mode requires --yes")
	}

	ctx := wizard.WizardContext{
		In:     cmd.InOrStdin(),
		Out:    cmd.OutOrStdout(),
		ErrOut: cmd.ErrOrStderr(),
		YesAll: flags.yesAll,
	}
	return wizard.RunInteractive(ctx, wizard.RunOptions{
		Output:  flags.output,
		Format:  flags.format,
		Force:   flags.force,
		NoMerge: flags.noMerge,
	})
}