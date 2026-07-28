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
		"install_flags.go",
		"wizard_test.go",
	} {
		if _, err := os.Stat(filepath.Join(wizardDir, name)); err != nil {
			t.Errorf("wizard command package missing %s: %v", name, err)
		}
	}

	promptDir := filepath.Join(wizardDir, "prompt")
	promptPkg, err := build.Default.ImportDir(promptDir, build.ImportComment)
	if err != nil {
		t.Fatalf("load wizard prompt package: %v", err)
	}
	if promptPkg.Name != "prompt" {
		t.Fatalf("wizard prompt package name = %q, want prompt", promptPkg.Name)
	}
	for _, name := range []string{
		"doc.go",
		"template.go",
		"system.go",
		"business.go",
		"template_test.go",
	} {
		if _, err := os.Stat(filepath.Join(promptDir, name)); err != nil {
			t.Errorf("wizard prompt package missing %s: %v", name, err)
		}
	}

	for _, name := range []string{"install_system.go", "install_business.go"} {
		if _, err := os.Stat(filepath.Join(wizardDir, name)); !os.IsNotExist(err) {
			t.Errorf("legacy wizard prompt file cmd/wizard/%s still exists", name)
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

func TestRootCommandLivesInCmdPackage(t *testing.T) {
	cmdDir := "cmd"
	pkg, err := build.Default.ImportDir(cmdDir, build.ImportComment)
	if err != nil {
		t.Fatalf("load root command package: %v", err)
	}
	if pkg.Name != "cmd" {
		t.Fatalf("root command package name = %q, want cmd", pkg.Name)
	}

	for _, name := range []string{"root.go", "execute.go", "root_test.go"} {
		if _, err := os.Stat(filepath.Join(cmdDir, name)); err != nil {
			t.Errorf("root command package missing %s: %v", name, err)
		}
	}

	runtimeDir := filepath.Join(cmdDir, "runtime")
	runtimePkg, err := build.Default.ImportDir(runtimeDir, build.ImportComment)
	if err != nil {
		t.Fatalf("load CLI runtime package: %v", err)
	}
	if runtimePkg.Name != "runtime" {
		t.Fatalf("CLI runtime package name = %q, want runtime", runtimePkg.Name)
	}
	for _, name := range []string{"state.go", "client.go", "client_autostart.go"} {
		if _, err := os.Stat(filepath.Join(runtimeDir, name)); err != nil {
			t.Errorf("CLI runtime package missing %s: %v", name, err)
		}
	}

	if _, err := os.Stat(filepath.Join(cmdDir, "root")); !os.IsNotExist(err) {
		t.Error("legacy cmd/root package still exists")
	}
}
