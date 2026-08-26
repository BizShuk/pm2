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

// TaskLogsDir returns the single directory every managed task logs into,
// relative to the pm2 state root (~/.config/pm2).
//
// One flat directory, not one per application: a task's log location is a
// property of pm2, not of the application it supervises. Scattering them
// under each application's own config directory made them impossible to
// list, size, or clean without walking the whole config root, and left a
// deleted task's logs stranded beside files pm2 never owned.
func TaskLogsDir(root string) string {
	return filepath.Join(root, "tasks", "logs")
}

// TaskLogPath returns the stdout log path for a task name.
func TaskLogPath(root, name string) string {
	return filepath.Join(TaskLogsDir(root), NormalizeName(name)+".log")
}

// TaskErrPath returns the stderr log path for a task name.
func TaskErrPath(root, name string) string {
	return filepath.Join(TaskLogsDir(root), NormalizeName(name)+".err")
}
