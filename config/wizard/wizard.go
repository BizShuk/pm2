package wizard

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bizshuk/pm2/process"
)

// RunOptions drives RunInteractive. Field semantics match the former
// CLI flags 1:1. All fields except the (Format, Output) defaults are
// optional.
type RunOptions struct {
	// Output is the destination file path. Empty -> resolved from
	// Format (FormatJS -> defaultOutput, FormatJSON -> .json variant).
	Output string
	// Format is FormatJS or FormatJSON. Empty -> FormatJS. Invalid
	// values are rejected with a clear error.
	Format string
	// Force replaces the destination file outright instead of
	// merging with existing apps.
	Force bool
	// NoMerge aborts when the destination file already exists.
	NoMerge bool
}

// DefaultRunOptions returns a RunOptions pre-populated with the
// default format (JS) and empty Output (RunInteractive resolves
// the output path from Format). Useful for callers that only need
// to override one or two fields.
func DefaultRunOptions() RunOptions {
	return RunOptions{Format: defaultFormat}
}

// InstallOptions drives RunInstall. Mirrors RunOptions but defaults
// Format to JS unconditionally — `wizard install` does not yet
// support JSON output.
type InstallOptions struct {
	Output  string
	Format  string
	Force   bool
	NoMerge bool
}

// DefaultInstallOptions returns an InstallOptions with Format=JS
// and Output left empty (RunInstall resolves to defaultOutput).
func DefaultInstallOptions() InstallOptions {
	return InstallOptions{Format: FormatJS}
}

// RunInteractive is the entry point for `pm2 wizard`. It validates
// the options, resolves the output path, then either:
//
//   - when ctx.YesAll is true: synthesises a single default app
//     (no prompting), or
//   - otherwise: walks the per-app question block in collectAnswers.
//
// The collected apps are then passed through WriteEcosystemFile,
// which handles the merge-vs-replace decision and the final "Write?"
// confirmation unless ctx.YesAll short-circuits it.
func RunInteractive(ctx WizardContext, opts RunOptions) error {
	if opts.Format == "" {
		opts.Format = defaultFormat
	}
	if opts.Format != FormatJS && opts.Format != FormatJSON {
		return fmt.Errorf("invalid --format %q (want js|json)", opts.Format)
	}
	if opts.Output == "" {
		if opts.Format == FormatJSON {
			opts.Output = "ecosystem.config.json"
		} else {
			opts.Output = defaultOutput
		}
	}

	var apps []process.AppConfig
	if ctx.YesAll {
		apps = []process.AppConfig{DefaultApp()}
	} else {
		var err error
		apps, err = collectAnswers(ctx.In, ctx.Out)
		if err != nil {
			return err
		}
	}

	return WriteEcosystemFile(ctx, apps, opts.Output, WriteOptions{
		Force:   opts.Force,
		NoMerge: opts.NoMerge,
		Format:  opts.Format,
	})
}

// RunInstall is the entry point for `pm2 wizard install`. It writes
// a single AppConfig through WriteEcosystemFile with ctx.YesAll
// forced to true so no "Write?" prompt is shown — the install flow
// is always non-interactive.
//
// Pass an already-populated AppConfig; the CLI layer is responsible
// for filling in the planner prefix, name, args, etc. before
// calling here. RunInstall only owns the output path resolution and
// the write step.
func RunInstall(ctx WizardContext, app process.AppConfig, opts InstallOptions) error {
	if opts.Format == "" {
		opts.Format = FormatJS
	}
	if opts.Format != FormatJS && opts.Format != FormatJSON {
		return fmt.Errorf("invalid --format %q (want js|json)", opts.Format)
	}
	if opts.Output == "" {
		opts.Output = defaultOutput
	}

	// Force non-interactive: install callers should never block on
	// a "Write?" prompt. ctx is passed through but YesAll is
	// overridden for this call only.
	installCtx := ctx
	installCtx.YesAll = true

	return WriteEcosystemFile(installCtx, []process.AppConfig{app}, opts.Output, WriteOptions{
		Force:   opts.Force,
		NoMerge: opts.NoMerge,
		Format:  opts.Format,
	})
}

// defaultApp returns a single AppConfig pre-filled with safe defaults.
func DefaultApp() process.AppConfig {
	a := process.AppConfig{
		Script:    defaultScript,
		Name:      defaultName,
		Namespace: defaultNamespace,
		Instances: 1,
		Version:   DefaultVersion,
	}
	a.Normalize("")
	return a
}

// collectAnswers walks the per-app question block and loops on "add another app?".
func collectAnswers(in io.Reader, out io.Writer) ([]process.AppConfig, error) {
	rdr := bufio.NewReader(in)
	var apps []process.AppConfig
	for n := 1; n <= maxApps; n++ {
		fmt.Fprintf(out, "\n=== App #%d ===\n", n)
		app, err := askOneApp(rdr, out)
		if err != nil {
			return nil, err
		}
		app.Normalize("")
		apps = append(apps, app)
		fmt.Fprintf(out, "  -> app #%d: name=%s script=%s instances=%d namespace=%s watch=%t cron=%q\n",
			n, app.Name, app.Script, app.Instances, app.Namespace, app.Watch, app.Cron)
		if n == maxApps {
			fmt.Fprintf(out, "(reached max of %d apps; stopping)\n", maxApps)
			break
		}
		more, err := promptYesNo(rdr, out, "Add another app?", false)
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
	}
	return apps, nil
}

// askOneApp runs the per-app question block for a single AppConfig.
func askOneApp(rdr *bufio.Reader, out io.Writer) (process.AppConfig, error) {
	var app process.AppConfig

	script, err := promptLine(rdr, out, "Script path", defaultScript)
	if err != nil {
		return app, err
	}
	if script == "" {
		script = defaultScript
	}
	if _, err := os.Stat(script); err != nil {
		fmt.Fprintf(out, "  (warning: %q not found locally — continuing anyway)\n", script)
	}
	app.Script = script

	name, err := promptLine(rdr, out, "Process name", DeriveName(script))
	if err != nil {
		return app, err
	}
	if name == "" {
		name = DeriveName(script)
	}
	app.Name = name

	argsRaw, err := promptLine(rdr, out, "Args (space-separated)", "")
	if err != nil {
		return app, err
	}
	if argsRaw != "" {
		app.Args = strings.Fields(argsRaw)
	}

	ns, err := promptLine(rdr, out, "Namespace", defaultNamespace)
	if err != nil {
		return app, err
	}
	if ns == "" {
		ns = defaultNamespace
	}
	app.Namespace = ns

	inst, err := promptInstances(rdr, out)
	if err != nil {
		return app, err
	}
	app.Instances = inst

	watch, err := promptYesNo(rdr, out, "Watch mode?", false)
	if err != nil {
		return app, err
	}
	app.Watch = watch

	env, err := promptEnvVars(rdr, out)
	if err != nil {
		return app, err
	}
	app.Env = env

	cron, err := promptLine(rdr, out, "Cron schedule (e.g. \"*/5 * * * *\", blank to skip)", "")
	if err != nil {
		return app, err
	}
	if cron != "" {
		app.Cron = cron
		restart, err := promptYesNo(rdr, out, "Cron restart (re-spawn the process on this schedule)?", false)
		if err != nil {
			return app, err
		}
		if restart {
			app.CronRestart = cron
		}
	}

	app.Version = DefaultVersion
	return app, nil
}

// DeriveName produces a process name from a script path. The
// derivation mirrors process.AppConfig.Normalize (see process/types.go):
// strip the path and the extension; fall back to defaultName when the
// script is empty or the basename has no stem.
//
// Exported because the cmd install subcommand and the daemon restart
// path both reuse the same rule — centralising it here keeps the
// names consistent across every code path that creates an AppConfig.
func DeriveName(script string) string {
	if script == "" {
		return defaultName
	}
	base := filepath.Base(script)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	if base == "" {
		return defaultName
	}
	return base
}