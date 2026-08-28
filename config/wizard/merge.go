package wizard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bizshuk/pm2/config"
	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/workflow"
)

// mergeCounts reports what the merge kept and what it dropped, per block.
type mergeCounts struct {
	appsSkipped      int
	workflowsSkipped int
}

func loadExisting(path string) (Ecosystem, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return Ecosystem{}, nil
		}
		return Ecosystem{}, fmt.Errorf("stat %s: %w", path, err)
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".json" && ext != ".js" && ext != ".cjs" && ext != ".mjs" {
		return Ecosystem{}, fmt.Errorf("unsupported existing file format %q (want .js or .json)", ext)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return Ecosystem{}, fmt.Errorf("parse existing %s: %w", path, err)
	}
	return Ecosystem{Apps: cfg.Apps, Workflows: cfg.Workflows}, nil
}

// mergeDocuments keeps every existing declaration and appends the new
// ones that do not collide. Identity is the app name for an app and the
// "<category>:<name>" key for a workflow — the same identities the
// daemon registers under, so a merge that looks clean here cannot
// produce a file the daemon rejects as a duplicate.
func mergeDocuments(existing, incoming Ecosystem) (Ecosystem, mergeCounts) {
	var (
		merged Ecosystem
		counts mergeCounts
	)

	seenApps := make(map[string]struct{}, len(existing.Apps))
	merged.Apps = make([]process.AppConfig, 0, len(existing.Apps)+len(incoming.Apps))
	for _, app := range existing.Apps {
		app.Normalize("")
		if app.Name == "" {
			continue
		}
		seenApps[app.Name] = struct{}{}
		merged.Apps = append(merged.Apps, app)
	}
	for _, app := range incoming.Apps {
		app.Normalize("")
		if app.Name == "" {
			continue
		}
		if _, duplicate := seenApps[app.Name]; duplicate {
			counts.appsSkipped++
			continue
		}
		seenApps[app.Name] = struct{}{}
		merged.Apps = append(merged.Apps, app)
	}

	seenWorkflows := make(map[string]struct{}, len(existing.Workflows))
	merged.Workflows = make([]workflow.Config, 0, len(existing.Workflows)+len(incoming.Workflows))
	for _, wf := range existing.Workflows {
		wf.Normalize("")
		if wf.Name == "" {
			continue
		}
		seenWorkflows[wf.Key()] = struct{}{}
		merged.Workflows = append(merged.Workflows, wf)
	}
	for _, wf := range incoming.Workflows {
		wf.Normalize("")
		if wf.Name == "" {
			continue
		}
		if _, duplicate := seenWorkflows[wf.Key()]; duplicate {
			counts.workflowsSkipped++
			continue
		}
		seenWorkflows[wf.Key()] = struct{}{}
		merged.Workflows = append(merged.Workflows, wf)
	}

	return merged, counts
}

func detectFormatFromExt(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return FormatJSON, true
	case ".js", ".cjs", ".mjs":
		return FormatJS, true
	default:
		return "", false
	}
}
