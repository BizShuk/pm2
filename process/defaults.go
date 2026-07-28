package process

import "path/filepath"

const (
	// DefaultNamespace is assigned when an app does not declare a namespace.
	DefaultNamespace = "default"
	// DefaultInstances is the fallback process instance count.
	DefaultInstances = 1
	// DefaultMaxRestarts is the fallback crash-restart limit.
	DefaultMaxRestarts = 15
)

// DefaultConfigDir returns the standard config directory for an app name.
func DefaultConfigDir(name string) string {
	return "~/.config/" + NormalizeName(name) + "/"
}

// DefaultLogFile returns the standard combined log path under configDir.
func DefaultLogFile(configDir string) string {
	return filepath.Join(configDir, "logs", "daemon.log")
}

// DefaultErrorFile returns the standard error log path under configDir.
func DefaultErrorFile(configDir string) string {
	return filepath.Join(configDir, "logs", "daemon.err")
}
