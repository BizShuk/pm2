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

func TestDefaultAppPaths(t *testing.T) {
	configDir := DefaultConfigDir("My App")
	if want := "~/.config/my-app/"; configDir != want {
		t.Fatalf("DefaultConfigDir() = %q, want %q", configDir, want)
	}
	if want := filepath.Join(configDir, "logs", "daemon.log"); DefaultLogFile(configDir) != want {
		t.Fatalf("DefaultLogFile() = %q, want %q", DefaultLogFile(configDir), want)
	}
	if want := filepath.Join(configDir, "logs", "daemon.err"); DefaultErrorFile(configDir) != want {
		t.Fatalf("DefaultErrorFile() = %q, want %q", DefaultErrorFile(configDir), want)
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
	if app.ConfigDir != DefaultConfigDir(app.Name) {
		t.Fatalf("ConfigDir = %q, want %q", app.ConfigDir, DefaultConfigDir(app.Name))
	}
	if app.LogFile != DefaultLogFile(app.ConfigDir) {
		t.Fatalf("LogFile = %q, want %q", app.LogFile, DefaultLogFile(app.ConfigDir))
	}
	if app.ErrorFile != DefaultErrorFile(app.ConfigDir) {
		t.Fatalf("ErrorFile = %q, want %q", app.ErrorFile, DefaultErrorFile(app.ConfigDir))
	}
}
