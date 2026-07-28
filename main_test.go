package main

import (
	"go/build"
	"os"
	"path/filepath"
	"testing"
)

func TestTaskCommandsLiveInSubpackage(t *testing.T) {
	taskDir := filepath.Join("cmd", "task")
	pkg, err := build.Default.ImportDir(taskDir, build.ImportComment)
	if err != nil {
		t.Fatalf("load task command package: %v", err)
	}
	if pkg.Name != "task" {
		t.Fatalf("task command package name = %q, want task", pkg.Name)
	}

	for _, name := range []string{
		"task.go",
		"start.go",
		"apply.go",
		"select.go",
		"single.go",
		"restart.go",
		"stop.go",
		"pause.go",
		"resume.go",
		"delete.go",
	} {
		if _, err := os.Stat(filepath.Join(taskDir, name)); err != nil {
			t.Errorf("task command package missing %s: %v", name, err)
		}
	}

	if _, err := os.Stat(filepath.Join(taskDir, "run.go")); !os.IsNotExist(err) {
		t.Errorf("legacy task alias file cmd/task/run.go still exists")
	}

	for _, name := range []string{
		"task.go",
		"task_start.go",
		"task_stop.go",
		"task_pause.go",
		"task_resume.go",
		"task_delete.go",
		"run.go",
		"start_select.go",
		"restart.go",
		"stop.go",
		"pause.go",
		"resume.go",
		"delete.go",
	} {
		if _, err := os.Stat(filepath.Join("cmd", name)); !os.IsNotExist(err) {
			t.Errorf("legacy task command file cmd/%s still exists", name)
		}
	}
}

func TestDaemonCommandsLiveInSubpackage(t *testing.T) {
	daemonDir := filepath.Join("cmd", "daemon")
	pkg, err := build.Default.ImportDir(daemonDir, build.ImportComment)
	if err != nil {
		t.Fatalf("load daemon command package: %v", err)
	}
	if pkg.Name != "daemon" {
		t.Fatalf("daemon command package name = %q, want daemon", pkg.Name)
	}

	for _, name := range []string{
		"daemon.go",
		"start.go",
		"start_alias.go",
		"kill.go",
		"stop.go",
		"status.go",
	} {
		if _, err := os.Stat(filepath.Join(daemonDir, name)); err != nil {
			t.Errorf("daemon command package missing %s: %v", name, err)
		}
	}

	for _, name := range []string{
		"daemon.go",
		"daemon_start.go",
		"daemon_kill.go",
		"daemon_stop.go",
		"daemon_status.go",
		"start.go",
	} {
		if _, err := os.Stat(filepath.Join("cmd", name)); !os.IsNotExist(err) {
			t.Errorf("legacy daemon command file cmd/%s still exists", name)
		}
	}
}

func TestWizardCommandsLiveInSubpackage(t *testing.T) {
	wizardDir := filepath.Join("cmd", "wizard")
	pkg, err := build.Default.ImportDir(wizardDir, build.ImportComment)
	if err != nil {
		t.Fatalf("load wizard command package: %v", err)
	}
	if pkg.Name != "wizard" {
		t.Fatalf("wizard command package name = %q, want wizard", pkg.Name)
	}

	for _, name := range []string{
		"wizard.go",
		"install.go",
		"install_system.go",
		"install_business.go",
		"wizard_test.go",
	} {
		if _, err := os.Stat(filepath.Join(wizardDir, name)); err != nil {
			t.Errorf("wizard command package missing %s: %v", name, err)
		}
	}

	for _, name := range []string{
		"eco.go",
		"eco_install.go",
		"eco_install_system.go",
		"eco_install_business.go",
		"eco_test.go",
	} {
		if _, err := os.Stat(filepath.Join("cmd", name)); !os.IsNotExist(err) {
			t.Errorf("legacy wizard command file cmd/%s still exists", name)
		}
	}
}

func TestCustomRootCommandLivesInSubpackage(t *testing.T) {
	rootDir := filepath.Join("cmd", "root")
	pkg, err := build.Default.ImportDir(rootDir, build.ImportComment)
	if err != nil {
		t.Fatalf("load custom root command package: %v", err)
	}
	if pkg.Name != "root" {
		t.Fatalf("custom root command package name = %q, want root", pkg.Name)
	}

	for _, name := range []string{"root.go", "execute.go", "root_test.go"} {
		if _, err := os.Stat(filepath.Join(rootDir, name)); err != nil {
			t.Errorf("custom root command package missing %s: %v", name, err)
		}
	}

	if _, err := os.Stat(filepath.Join("cmd", "state.go")); err != nil {
		t.Errorf("shared CLI state file cmd/state.go is missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join("cmd", "root.go")); !os.IsNotExist(err) {
		t.Error("legacy shared-state file cmd/root.go still exists")
	}
}
