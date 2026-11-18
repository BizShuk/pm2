package wizard

import (
	"path/filepath"
	"strings"

	"github.com/bizshuk/pm2/process"
)

// formatWizardName turns the prompted purpose into a valid task name
// `<Purpose> - <Frequency>`, the frequency read from app's schedule.
func formatWizardName(purpose string, app process.AppConfig) string {
	return process.FormatTaskName(purpose, app)
}

// DeriveName produces a process name from a script path.
func DeriveName(script string) string {
	if script == "" {
		return defaultName
	}
	base := filepath.Base(script)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "" {
		return defaultName
	}
	return base
}
