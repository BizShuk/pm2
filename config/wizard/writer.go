package wizard

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bizshuk/pm2/process"
)

// WriteEcosystemFile merges or replaces an ecosystem file, previews the
// result, confirms interactive writes, and persists the selected format.
func WriteEcosystemFile(ctx WizardContext, apps []process.AppConfig, opts WriteOptions) error {
	format, err := normalizeFormat(opts.Format)
	if err != nil {
		return err
	}
	if opts.Output == "" {
		opts.Output = defaultOutputFor(format)
	}
	output := opts.Output

	_, statErr := os.Stat(output)
	exists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return fmt.Errorf("stat %s: %w", output, statErr)
	}

	appsToWrite := apps
	writeFormat := format
	skipped := 0

	if exists && !opts.Force {
		if opts.NoMerge {
			return fmt.Errorf(
				"refusing to overwrite existing %s; use --force to replace or remove --no-merge to merge",
				output,
			)
		}
		existing, err := loadExistingApps(output)
		if err != nil {
			return fmt.Errorf("%w (use --force to overwrite a broken file)", err)
		}
		if detected, ok := detectFormatFromExt(output); ok {
			writeFormat = detected
		}
		appsToWrite, skipped = mergeAppsByName(existing, apps)
	}

	data, err := renderEcosystem(appsToWrite, writeFormat)
	if err != nil {
		return err
	}

	summary := writeSummary(exists, opts.Force, len(apps), len(appsToWrite), skipped)
	fmt.Fprintf(
		ctx.ErrOut,
		"\n--- preview of %s ---\n%s\n--- end preview (%s) ---\n",
		output,
		data,
		summary,
	)

	if !ctx.YesAll {
		ok, err := promptYesNo(
			bufio.NewReader(ctx.In),
			ctx.Out,
			fmt.Sprintf("Write to file %s?", output),
			true,
		)
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

func renderEcosystem(apps []process.AppConfig, format string) ([]byte, error) {
	if format == FormatJSON {
		data, err := renderEcosystemJSON(apps)
		return []byte(data), err
	}
	return []byte(renderEcosystemJS(apps)), nil
}

func writeSummary(exists, force bool, newCount, mergedCount, skipped int) string {
	switch {
	case force:
		return fmt.Sprintf("replace with %d app(s)", mergedCount)
	case exists:
		existingCount := mergedCount - newCount + skipped
		return fmt.Sprintf(
			"merged %d existing + %d new = %d (skipped %d duplicate name(s))",
			existingCount,
			newCount-skipped,
			mergedCount,
			skipped,
		)
	default:
		return fmt.Sprintf("%d app(s) to write", mergedCount)
	}
}
