package process

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// NormalizeName returns a filesystem-safe form of a process name for
// use as a path component: lowercased with spaces rewritten to hyphens.
// Used as the default ConfigDir segment when the user does not supply
// one (e.g. process "My App" → "my-app" → "~/.config/my-app/").
func NormalizeName(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", "-"))
}

// ResolveScriptPath resolves a possibly-relative script path against
// baseDir. Absolute paths and bare commands resolvable via $PATH pass
// through unchanged. Lives in the process package so the AppConfig
// normalizer can call it without creating an import cycle.
func ResolveScriptPath(baseDir, script string) string {
	if script == "" || filepath.IsAbs(script) {
		return script
	}
	if filepath.Base(script) != script || strings.Contains(script, "/") || strings.Contains(script, string(filepath.Separator)) {
		return filepath.Join(baseDir, script)
	}
	targetPath := filepath.Join(baseDir, script)
	if _, err := os.Stat(targetPath); err == nil {
		return targetPath
	}
	if lookPath, err := exec.LookPath(script); err == nil {
		if absPath, err := filepath.Abs(lookPath); err == nil {
			return absPath
		}
		return lookPath
	}
	return script
}
