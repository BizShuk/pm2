package wizard

import (
	"fmt"
	"path/filepath"
	"strings"
)

func formatWizardName(namespace, script, name string) string {
	return strings.ToUpper(fmt.Sprintf(
		"%s %s - %s",
		strings.TrimSpace(namespace),
		DeriveName(strings.TrimSpace(script)),
		strings.TrimSpace(name),
	))
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
