package wizard

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
)

// WriteEcosystemFile merges or replaces an ecosystem file, previews the
// result, confirms interactive writes, and persists the selected format.
func WriteEcosystemFile(ctx WizardContext, doc Ecosystem, opts WriteOptions) error {
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

	toWrite := doc
	writeFormat := format
	var counts mergeCounts

	if exists && !opts.Force {
		if opts.NoMerge {
			return fmt.Errorf(
				"refusing to overwrite existing %s; use --force to replace or remove --no-merge to merge",
				output,
			)
		}
		existing, err := loadExisting(output)
		if err != nil {
			return fmt.Errorf("%w (use --force to overwrite a broken file)", err)
		}
		if detected, ok := detectFormatFromExt(output); ok {
			writeFormat = detected
		}
		toWrite, counts = mergeDocuments(existing, doc)
	}

	if err := validateDocument(toWrite); err != nil {
		return err
	}

	data, err := renderEcosystem(toWrite, writeFormat)
	if err != nil {
		return err
	}

	summary := writeSummary(exists, opts.Force, doc, toWrite, counts)
	fmt.Fprintf(
		ctx.ErrOut,
		"\n--- preview of %s ---\n%s\n--- end preview (%s) ---\n",
		output,
		data,
		summary,
	)
	warnDanglingRefs(ctx.ErrOut, toWrite)

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

func renderEcosystem(doc Ecosystem, format string) ([]byte, error) {
	if format == FormatJSON {
		data, err := renderEcosystemJSON(doc)
		return []byte(data), err
	}
	return []byte(renderEcosystemJS(doc)), nil
}

func writeSummary(exists, force bool, collected, merged Ecosystem, counts mergeCounts) string {
	apps := fmt.Sprintf("%d app(s)", len(merged.Apps))
	if len(merged.Workflows) > 0 {
		apps += fmt.Sprintf(" + %d workflow(s)", len(merged.Workflows))
	}

	switch {
	case force:
		return "replace with " + apps
	case exists:
		existingApps := len(merged.Apps) - len(collected.Apps) + counts.appsSkipped
		summary := fmt.Sprintf(
			"merged %d existing + %d new = %d app(s) (skipped %d duplicate name(s))",
			existingApps,
			len(collected.Apps)-counts.appsSkipped,
			len(merged.Apps),
			counts.appsSkipped,
		)
		if len(merged.Workflows) > 0 || len(collected.Workflows) > 0 {
			existingWorkflows := len(merged.Workflows) -
				len(collected.Workflows) + counts.workflowsSkipped
			summary += fmt.Sprintf(
				"; merged %d existing + %d new = %d workflow(s) (skipped %d duplicate key(s))",
				existingWorkflows,
				len(collected.Workflows)-counts.workflowsSkipped,
				len(merged.Workflows),
				counts.workflowsSkipped,
			)
		}
		return summary
	default:
		return apps + " to write"
	}
}
