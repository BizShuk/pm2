package process

import (
	"path/filepath"
	"testing"
)

func TestAppConfigDefaults(t *testing.T) {
	if DefaultNamespace != "default" {
		t.Fatalf("DefaultNamespace = %q, want default", DefaultNamespace)
	}
	if DefaultInstances != 1 {
		t.Fatalf("DefaultInstances = %d, want 1", DefaultInstances)
	}
	if DefaultMaxRestarts != 15 {
		t.Fatalf("DefaultMaxRestarts = %d, want 15", DefaultMaxRestarts)
	}
}

func TestTaskLogPaths(t *testing.T) {
	root := "/state"
	if want := filepath.Join(root, "tasks", "logs"); TaskLogsDir(root) != want {
		t.Fatalf("TaskLogsDir() = %q, want %q", TaskLogsDir(root), want)
	}
	// The task name is normalized into the filename, so a name with spaces
	// or capitals cannot produce a path the shell has to quote.
	if want := filepath.Join(root, "tasks", "logs", "my-app.log"); TaskLogPath(root, "My App") != want {
		t.Fatalf("TaskLogPath() = %q, want %q", TaskLogPath(root, "My App"), want)
	}
	if want := filepath.Join(root, "tasks", "logs", "my-app.err"); TaskErrPath(root, "My App") != want {
		t.Fatalf("TaskErrPath() = %q, want %q", TaskErrPath(root, "My App"), want)
	}
}

func TestNormalizeUsesSharedDefaults(t *testing.T) {
	app := AppConfig{Name: "My App"}
	app.Normalize("")

	if app.Namespace != DefaultNamespace {
		t.Fatalf("Namespace = %q, want %q", app.Namespace, DefaultNamespace)
	}
	if app.Instances != DefaultInstances {
		t.Fatalf("Instances = %d, want %d", app.Instances, DefaultInstances)
	}
	if app.MaxRestarts != DefaultMaxRestarts {
		t.Fatalf("MaxRestarts = %d, want %d", app.MaxRestarts, DefaultMaxRestarts)
	}
}
