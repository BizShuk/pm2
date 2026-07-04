package wizard

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bizshuk/pm2/config"
	"github.com/bizshuk/pm2/process"
)

// Default file names and limits used by the wizard's interactive
// prompts and rendering helpers. Kept unexported so external callers
// don't bind to specific magic values — they only see the public
// RunInteractive / RunInstall entry points and the Format* constants.
const (
	defaultOutput    = "ecosystem.config.js"
	defaultFormat    = FormatJS
	maxApps          = 64
	defaultScript    = "app.js"
	defaultName      = "app"
	defaultNamespace = "default"
)

// WriteOptions controls how WriteEcosystemFile decides whether to
// merge, replace, or refuse an existing output file. Field semantics
// match the former CLI flags 1:1.
type WriteOptions struct {
	// Force replaces the entire output file with the newly collected
	// apps, bypassing both the merge step and the parse check on the
	// existing file.
	Force bool
	// NoMerge aborts when the output file already exists. Combine
	// with Force to force-replace.
	NoMerge bool
	// Format is "js" or "json". When the output file already exists
	// its extension wins on merge; Format only governs brand-new
	// files or --force replacement.
	Format string
}

// DefaultWriteOptions returns sane defaults: JS format, no force, no
// no-merge. Callers that need different behaviour override fields.
func DefaultWriteOptions() WriteOptions {
	return WriteOptions{Format: defaultFormat}
}

// WriteEcosystemFile is the shared merge-or-replace-then-write step
// used by both the interactive wizard and the install subcommand.
//
// Flow:
//
//  1. If output does not exist -> write apps verbatim.
//  2. If output exists and Force -> replace.
//  3. If output exists and NoMerge -> error.
//  4. Otherwise: load existing apps, merge by name (existing wins),
//     emit a preview to ctx.ErrOut, prompt for confirmation unless
//     ctx.YesAll is set, then write.
//
// The "Write?" confirmation reads from ctx.In via a bufio.Reader; if
// the caller pre-set ctx.YesAll (non-interactive install, or
// --yes/--format pipes) the prompt is skipped and the file is
// written unconditionally.
func WriteEcosystemFile(ctx WizardContext, apps []process.AppConfig, output string, opts WriteOptions) error {
	if opts.Format == "" {
		opts.Format = defaultFormat
	}

	var (
		mergedApps []process.AppConfig
		skipped    int
		writeFmt   = opts.Format
	)
	if _, statErr := os.Stat(output); statErr == nil {
		if opts.Force {
			mergedApps = apps
		} else if opts.NoMerge {
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
	case FormatJSON:
		s, err := renderEcosystemJSON(mergedApps)
		if err != nil {
			return err
		}
		data = []byte(s)
	default:
		data = []byte(renderEcosystemJS(mergedApps))
	}

	summary := fmt.Sprintf("%d app(s) to write", len(mergedApps))
	if opts.Force {
		summary = fmt.Sprintf("replace with %d app(s)", len(mergedApps))
	} else if _, statErr := os.Stat(output); statErr == nil {
		summary = fmt.Sprintf(
			"merged %d existing + %d new = %d (skipped %d duplicate name(s))",
			len(mergedApps)-len(apps)+skipped, len(apps)-skipped,
			len(mergedApps), skipped)
	}
	fmt.Fprintf(ctx.ErrOut, "\n--- preview of %s ---\n%s\n--- end preview (%s) ---\n",
		output, data, summary)

	if !ctx.YesAll {
		rdr := bufio.NewReader(ctx.In)
		ok, err := promptYesNo(rdr, ctx.Out, fmt.Sprintf("Write %s?", output), true)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(ctx.Out, "Aborted.")
			return nil
		}
	}

	if err := os.WriteFile(output, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", output, err)
	}
	abs, _ := filepath.Abs(output)
	fmt.Fprintf(ctx.Out, "Wrote %s\n", abs)
	return nil
}

// loadExistingApps reads an existing ecosystem file at path and returns
// the apps declared in it. Returns:
//   - (nil, nil)  if the file does not exist
//   - (nil, err)  if the file exists but is malformed / unreadable
//   - (apps, nil) on success
func loadExistingApps(path string) ([]process.AppConfig, error) {
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
// the AppConfig.Name field. Existing apps win on name collision.
// Names are compared after Normalize() so "api" and "" both map to
// the derived form consistently.
func mergeAppsByName(existing, newApps []process.AppConfig) (merged []process.AppConfig, skipped int) {
	seen := make(map[string]struct{}, len(existing))
	merged = make([]process.AppConfig, 0, len(existing)+len(newApps))
	for i := range existing {
		existing[i].Normalize("")
		a := existing[i]
		if a.Name == "" {
			continue
		}
		seen[a.Name] = struct{}{}
		merged = append(merged, a)
	}
	for i := range newApps {
		newApps[i].Normalize("")
		a := newApps[i]
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

// detectFormatFromExt returns FormatJS or FormatJSON based on the
// file extension. Unrecognized extensions yield ("", false) so the
// caller can fall back to the user-supplied --format.
func detectFormatFromExt(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return FormatJSON, true
	case ".js", ".cjs", ".mjs":
		return FormatJS, true
	}
	return "", false
}

// renderEcosystemJS emits the canonical PM2 JS form. Skips zero-value
// fields except `script`. Uses 4-space indent and double quotes.
func renderEcosystemJS(apps []process.AppConfig) string {
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

func writeAppJS(b *strings.Builder, a process.AppConfig) {
	ns := a.Namespace
	if ns == "" {
		ns = defaultNamespace
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
		keys := make([]string, 0, len(a.Env))
		for k := range a.Env {
			keys = append(keys, k)
		}
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

// renderEcosystemJSON is the JSON counterpart for FormatJSON.
func renderEcosystemJSON(apps []process.AppConfig) (string, error) {
	cfg := config.EcosystemConfig{Apps: apps}
	data, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}