package cmd

import (
	"fmt"
	"os"

	"github.com/bizshuk/pm2/config"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

const (
	ecoDefaultOutput  = "ecosystem.config.js"
	ecoFormatJS       = "js"
	ecoFormatJSON     = "json"
	ecoMaxApps        = 64
	ecoDefaultScript  = "app.js"
	ecoDefaultName    = "app"
	ecoDefaultNS      = "default"
	ecoDefaultVersion = "-"
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
	return interactiveFlags{format: ecoFormatJS}
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

func runInteractive(cmd *cobra.Command, flags *interactiveFlags) error {
	if flags.format != ecoFormatJS && flags.format != ecoFormatJSON {
		return fmt.Errorf("invalid --format %q (want js|json)", flags.format)
	}
	if flags.output == "" {
		if flags.format == ecoFormatJSON {
			flags.output = "ecosystem.config.json"
		} else {
			flags.output = ecoDefaultOutput
		}
	}

	in := cmd.InOrStdin()
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	tty := isTerminalFunc(os.Stdin.Fd())
	if !tty && !flags.yesAll {
		fmt.Fprintln(errOut,
			"pm2 eco requires an interactive terminal. "+
				"Re-run with --yes to generate a config with all defaults.")
		return fmt.Errorf("non-interactive mode requires --yes")
	}

	var apps []config.AppConfig
	if flags.yesAll {
		apps = []config.AppConfig{defaultApp()}
	} else {
		var err error
		apps, err = collectAnswers(in, out)
		if err != nil {
			return err
		}
	}

	return writeEcosystemFile(apps, flags.output, flags.force, flags.noMerge, flags.format, in, out, errOut, flags.yesAll)
}

// writeEcosystemFile is the shared merge-or-replace-then-write step
// used by both the interactive wizard and the `install` subcommand.
// `yesAll=true` skips the interactive "Write?" confirm prompt (used
