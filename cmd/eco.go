package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
// by non-interactive callers like `install`). Returns the list of
// names that were actually written to the file.
func writeEcosystemFile(apps []config.AppConfig, output string, force, noMerge bool, format string, in io.Reader, out, errOut io.Writer, yesAll bool) error {
	var (
		mergedApps []config.AppConfig
		skipped    int
		writeFmt   = format
	)
	if _, statErr := os.Stat(output); statErr == nil {
		// File exists.
		if force {
			mergedApps = apps
		} else if noMerge {
			return fmt.Errorf(
				"refusing to overwrite existing %s; use --force to replace "+
					"or remove --no-merge to merge", output)
		} else {
			existing, lerr := loadExistingApps(output)
			if lerr != nil {
				return fmt.Errorf(
					"%w (use --force to overwrite a broken file)", lerr)
			}
			if f, ok := detectFormatFromExt(output); ok {
				writeFmt = f
			}
			mergedApps, skipped = mergeAppsByName(existing, apps)
		}
	} else {
		mergedApps = apps
	}

	var data []byte
	switch writeFmt {
	case ecoFormatJSON:
		s, err := renderEcosystemJSON(mergedApps)
		if err != nil {
			return err
		}
		data = []byte(s)
	default:
		data = []byte(renderEcosystemJS(mergedApps))
	}

	summary := fmt.Sprintf("%d app(s) to write", len(mergedApps))
	if force {
		summary = fmt.Sprintf("replace with %d app(s)", len(mergedApps))
	} else if _, statErr := os.Stat(output); statErr == nil {
		summary = fmt.Sprintf(
			"merged %d existing + %d new = %d (skipped %d duplicate name(s))",
			len(mergedApps)-len(apps)+skipped, len(apps)-skipped,
			len(mergedApps), skipped)
	}
	fmt.Fprintf(errOut, "\n--- preview of %s ---\n%s\n--- end preview (%s) ---\n",
		output, data, summary)

	if !yesAll {
		rdr := bufio.NewReader(in)
		ok, err := promptYesNo(rdr, out, fmt.Sprintf("Write %s?", output), true)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(out, "Aborted.")
			return nil
		}
	}

	if err := os.WriteFile(output, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", output, err)
	}
	abs, _ := filepath.Abs(output)
	fmt.Fprintf(out, "Wrote %s\n", abs)
	return nil
}

// loadExistingApps reads an existing ecosystem file at path and returns
// the apps declared in it. Returns:
//   - (nil, nil)  if the file does not exist
//   - (nil, err)  if the file exists but is malformed / unreadable
//   - (apps, nil) on success
//
// Used by the wizard's merge path. We deliberately do NOT swallow
// parse errors: silently treating a broken file as empty would
// destroy user config on the next save. Callers should surface the
// error and point the user at --force.
func loadExistingApps(path string) ([]config.AppConfig, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".json" && ext != ".js" && ext != ".cjs" && ext != ".mjs" {
		return nil, fmt.Errorf("unsupported existing file format %q (want .js or .json)", ext)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("parse existing %s: %w", path, err)
	}
	return cfg.Apps, nil
}

// mergeAppsByName returns the union of existing + new apps, deduped by
// the AppConfig.Name field. Existing apps win on name collision
// (new apps whose name matches an existing one are dropped). Names
// are compared after Normalize() so "api" and "" both map to the
// derived form consistently.
//
// Returns the merged slice plus a count of how many new apps were
// skipped as duplicates, for the preview summary.
func mergeAppsByName(existing, newApps []config.AppConfig) (merged []config.AppConfig, skipped int) {
	seen := make(map[string]struct{}, len(existing))
	merged = make([]config.AppConfig, 0, len(existing)+len(newApps))
	for _, a := range existing {
		a.Normalize()
		if a.Name == "" {
			continue // skip degenerate entries
		}
		seen[a.Name] = struct{}{}
		merged = append(merged, a)
	}
	for _, a := range newApps {
		a.Normalize()
		if a.Name == "" {
			continue
		}
		if _, dup := seen[a.Name]; dup {
			skipped++
			continue
		}
		seen[a.Name] = struct{}{}
		merged = append(merged, a)
	}
	return merged, skipped
}

// detectFormatFromExt returns "js" or "json" based on the file
// extension. Unrecognized extensions yield ("", false) so the caller
// can fall back to the user-supplied --format.
func detectFormatFromExt(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return ecoFormatJSON, true
	case ".js", ".cjs", ".mjs":
		return ecoFormatJS, true
	}
	return "", false
}

// defaultApp returns a single AppConfig pre-filled with safe defaults.
func defaultApp() config.AppConfig {
	a := config.AppConfig{
		Script:    ecoDefaultScript,
		Name:      ecoDefaultName,
		Namespace: ecoDefaultNS,
		Instances: 1,
		Version:   ecoDefaultVersion,
	}
	a.Normalize()
	return a
}

// promptLine reads a single line, trims whitespace, returns it.
// Empty input == def. EOF returns an error.
func promptLine(rdr *bufio.Reader, out io.Writer, label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}
	line, err := rdr.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	if err == io.EOF && line == "" {
		// graceful EOF with no input — treat as empty
		return "", nil
	}
	line = strings.TrimRight(line, "\r\n")
	return line, nil
}

// promptYesNo accepts y/yes/n/no (case-insensitive). Empty == def.
func promptYesNo(rdr *bufio.Reader, out io.Writer, label string, def bool) (bool, error) {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	fmt.Fprintf(out, "%s [%s]: ", label, hint)
	line, err := rdr.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	line = strings.TrimSpace(strings.TrimRight(line, "\r\n"))
	if line == "" {
		return def, nil
	}
	switch strings.ToLower(line) {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	}
	return def, nil
}

// promptInstances reads an int with a soft retry loop; falls back to 1.
func promptInstances(rdr *bufio.Reader, out io.Writer) (int, error) {
	for i := 0; i < 3; i++ {
		s, err := promptLine(rdr, out, "Instances", "1")
		if err != nil {
			return 0, err
		}
		if s == "" {
			return 1, nil
		}
		n, perr := strconv.Atoi(strings.TrimSpace(s))
		if perr == nil && n > 0 {
			return n, nil
		}
		fmt.Fprintln(out, "  (invalid number, try again)")
	}
	return 1, nil
}

// promptEnvVars loops reading KEY=VAL until blank line.
func promptEnvVars(rdr *bufio.Reader, out io.Writer) (map[string]string, error) {
	env := make(map[string]string)
	fmt.Fprintln(out, "Env vars? (one per line KEY=VAL; blank line to finish)")
	for {
		s, err := promptLine(rdr, out, "  env", "")
		if err != nil {
			return nil, err
		}
		if s == "" {
			break
		}
		parts := strings.SplitN(s, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			fmt.Fprintf(out, "  (ignoring malformed env: %q)\n", s)
			continue
		}
		env[strings.TrimSpace(parts[0])] = parts[1]
	}
	if len(env) == 0 {
		return nil, nil
	}
	return env, nil
}

// collectAnswers walks the per-app question block and loops on "add another app?".
func collectAnswers(in io.Reader, out io.Writer) ([]config.AppConfig, error) {
	// Single shared buffered reader so all prompts see a consistent
	// stream position (each bufio.Reader pre-reads into its own buffer).
	rdr := bufio.NewReader(in)
	var apps []config.AppConfig
	for n := 1; n <= ecoMaxApps; n++ {
		fmt.Fprintf(out, "\n=== App #%d ===\n", n)
		app, err := askOneApp(rdr, out)
		if err != nil {
			return nil, err
		}
		app.Normalize()
		apps = append(apps, app)
		fmt.Fprintf(out, "  -> app #%d: name=%s script=%s instances=%d namespace=%s watch=%t cron=%q\n",
			n, app.Name, app.Script, app.Instances, app.Namespace, app.Watch, app.Cron)
		if n == ecoMaxApps {
			fmt.Fprintf(out, "(reached max of %d apps; stopping)\n", ecoMaxApps)
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
func askOneApp(rdr *bufio.Reader, out io.Writer) (config.AppConfig, error) {
	var app config.AppConfig

	script, err := promptLine(rdr, out, "Script path", ecoDefaultScript)
	if err != nil {
		return app, err
	}
	if script == "" {
		script = ecoDefaultScript
	}
	if _, err := os.Stat(script); err != nil {
		fmt.Fprintf(out, "  (warning: %q not found locally — continuing anyway)\n", script)
	}
	app.Script = script

	name, err := promptLine(rdr, out, "Process name", deriveName(script))
	if err != nil {
		return app, err
	}
	if name == "" {
		name = deriveName(script)
	}
	app.Name = name

	argsRaw, err := promptLine(rdr, out, "Args (space-separated)", "")
	if err != nil {
		return app, err
	}
	if argsRaw != "" {
		app.Args = strings.Fields(argsRaw)
	}

	ns, err := promptLine(rdr, out, "Namespace", ecoDefaultNS)
	if err != nil {
		return app, err
	}
	if ns == "" {
		ns = ecoDefaultNS
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

	app.Version = ecoDefaultVersion
	return app, nil
}

// deriveName produces a process name from a script path, matching
// config.AppConfig.Normalize (cmd/ecosystem.go:50-53).
func deriveName(script string) string {
	if script == "" {
		return ecoDefaultName
	}
	base := filepath.Base(script)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	if base == "" {
		return ecoDefaultName
	}
	return base
}

// renderEcosystemJS emits the canonical PM2 JS form. Skips zero-value
// fields except `script`. Uses 4-space indent and double quotes
// (matches README example at README.md:220-232).
func renderEcosystemJS(apps []config.AppConfig) string {
	var b strings.Builder
	b.WriteString("module.exports = {\n")
	b.WriteString("    apps: [\n")
	for i, a := range apps {
		writeAppJS(&b, a)
		if i < len(apps)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("    ],\n")
	b.WriteString("};\n")
	return b.String()
}

func writeAppJS(b *strings.Builder, a config.AppConfig) {
	// Comment header
	ns := a.Namespace
	if ns == "" {
		ns = ecoDefaultNS
	}
	fmt.Fprintf(b, "        // %s (%s)\n", a.Name, ns)

	b.WriteString("        {\n")
	if a.Name != "" {
		fmt.Fprintf(b, "            name: %s,\n", strconv.Quote(a.Name))
	}
	fmt.Fprintf(b, "            script: %s,\n", strconv.Quote(a.Script))
	if len(a.Args) > 0 {
		b.WriteString("            args: [")
		for i, arg := range a.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Quote(arg))
		}
		b.WriteString("],\n")
	}
	if a.Namespace != "" {
		fmt.Fprintf(b, "            namespace: %s,\n", strconv.Quote(a.Namespace))
	}
	if a.CWD != "" {
		fmt.Fprintf(b, "            cwd: %s,\n", strconv.Quote(a.CWD))
	}
	inst := a.Instances
	if inst <= 0 {
		inst = 1
	}
	fmt.Fprintf(b, "            instances: %d,\n", inst)
	if a.Watch {
		b.WriteString("            watch: true,\n")
	}
	if len(a.Env) > 0 {
		b.WriteString("            env: {\n")
		// stable key order
		keys := make([]string, 0, len(a.Env))
		for k := range a.Env {
			keys = append(keys, k)
		}
		// insertion sort (small N)
		for i := 1; i < len(keys); i++ {
			for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
				keys[j-1], keys[j] = keys[j], keys[j-1]
			}
		}
		for i, k := range keys {
			fmt.Fprintf(b, "                %s: %s", strconv.Quote(k), strconv.Quote(a.Env[k]))
			if i < len(keys)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString("            },\n")
	}
	if a.CronRestart != "" {
		fmt.Fprintf(b, "            cron_restart: %s,\n", strconv.Quote(a.CronRestart))
	}
	if a.Cron != "" {
		fmt.Fprintf(b, "            cron: %s,\n", strconv.Quote(a.Cron))
	}
	b.WriteString("        }")
}

// renderEcosystemJSON is the JSON counterpart for --format json.
func renderEcosystemJSON(apps []config.AppConfig) (string, error) {
	cfg := config.EcosystemConfig{Apps: apps}
	data, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}
