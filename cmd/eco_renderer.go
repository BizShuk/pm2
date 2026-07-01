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
	"github.com/bizshuk/pm2/process"
)

// writeEcosystemFile is the shared merge-or-replace-then-write step
// used by both the interactive wizard and the `install` subcommand.
// `yesAll=true` skips the interactive "Write?" confirm prompt (used
// by non-interactive callers like `install`). Returns the list of
// names that were actually written to the file.
func writeEcosystemFile(apps []process.AppConfig, output string, force, noMerge bool, format string, in io.Reader, out, errOut io.Writer, yesAll bool) error {
	var (
		mergedApps []process.AppConfig
		skipped    int
		writeFmt   = format
	)
	if _, statErr := os.Stat(output); statErr == nil {
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
// Names are compared after Normalize() so "api" and "" both map to the
// derived form consistently.
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

// detectFormatFromExt returns "js" or "json" based on the file extension.
// Unrecognized extensions yield ("", false) so the caller can fall back
// to the user-supplied --format.
func detectFormatFromExt(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return ecoFormatJSON, true
	case ".js", ".cjs", ".mjs":
		return ecoFormatJS, true
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

// renderEcosystemJSON is the JSON counterpart for --format json.
func renderEcosystemJSON(apps []process.AppConfig) (string, error) {
	cfg := config.EcosystemConfig{Apps: apps}
	data, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}