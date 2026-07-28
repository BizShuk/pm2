package wizard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bizshuk/pm2/config"
	"github.com/bizshuk/pm2/process"
)

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

func mergeAppsByName(existing, newApps []process.AppConfig) (merged []process.AppConfig, skipped int) {
	seen := make(map[string]struct{}, len(existing))
	merged = make([]process.AppConfig, 0, len(existing)+len(newApps))

	for _, app := range existing {
		app.Normalize("")
		if app.Name == "" {
			continue
		}
		seen[app.Name] = struct{}{}
		merged = append(merged, app)
	}
	for _, app := range newApps {
		app.Normalize("")
		if app.Name == "" {
			continue
		}
		if _, duplicate := seen[app.Name]; duplicate {
			skipped++
			continue
		}
		seen[app.Name] = struct{}{}
		merged = append(merged, app)
	}
	return merged, skipped
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
