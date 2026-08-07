package main

import (
	"go/build"
	"os"
	"path/filepath"
	"testing"
)

// Layout convention:
//   - First-layer commands are files in cmd/ (package cmd).
//   - Their subcommands live in cmd/<command>/<subcommand>.go.

func TestTaskCommandsLayout(t *testing.T) {
	// Parent first-layer command
	if _, err := os.Stat(filepath.Join("cmd", "task.go")); err != nil {
		t.Errorf("first-layer task command missing cmd/task.go: %v", err)
	}
	if _, err := os.Stat(filepath.Join("cmd", "apply.go")); err != nil {
		t.Errorf("first-layer apply alias missing cmd/apply.go: %v", err)
	}

	taskDir := filepath.Join("cmd", "task")
	pkg, err := build.Default.ImportDir(taskDir, build.ImportComment)
	if err != nil {
		t.Fatalf("load task command package: %v", err)
	}
	if pkg.Name != "task" {
		t.Fatalf("task command package name = %q, want task", pkg.Name)
	}

	for _, name := range []string{
		"start.go",
		"select.go",
		"single.go",
		"restart.go",
		"stop.go",
		"pause.go",
		"resume.go",
		"delete.go",
		"apply_delete.go",
	} {
		if _, err := os.Stat(filepath.Join(taskDir, name)); err != nil {
			t.Errorf("task subcommand package missing %s: %v", name, err)
		}
	}

	// Parent command must not live inside the subpackage.
	if _, err := os.Stat(filepath.Join(taskDir, "task.go")); !os.IsNotExist(err) {
		t.Errorf("parent command must live at cmd/task.go, not cmd/task/task.go")
	}
	if _, err := os.Stat(filepath.Join(taskDir, "apply.go")); !os.IsNotExist(err) {
		t.Errorf("root apply alias must live at cmd/apply.go, not cmd/task/apply.go")
	}

	for _, name := range []string{
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

func TestDaemonCommandsLayout(t *testing.T) {
	if _, err := os.Stat(filepath.Join("cmd", "daemon.go")); err != nil {
		t.Errorf("first-layer daemon command missing cmd/daemon.go: %v", err)
	}
	if _, err := os.Stat(filepath.Join("cmd", "start.go")); err != nil {
		t.Errorf("first-layer start alias missing cmd/start.go: %v", err)
	}

	daemonDir := filepath.Join("cmd", "daemon")
	pkg, err := build.Default.ImportDir(daemonDir, build.ImportComment)
	if err != nil {
		t.Fatalf("load daemon command package: %v", err)
	}
	if pkg.Name != "daemon" {
		t.Fatalf("daemon command package name = %q, want daemon", pkg.Name)
	}

	for _, name := range []string{
		"start.go",
		"kill.go",
		"stop.go",
		"status.go",
	} {
		if _, err := os.Stat(filepath.Join(daemonDir, name)); err != nil {
			t.Errorf("daemon subcommand package missing %s: %v", name, err)
		}
	}

	if _, err := os.Stat(filepath.Join(daemonDir, "daemon.go")); !os.IsNotExist(err) {
		t.Errorf("parent command must live at cmd/daemon.go, not cmd/daemon/daemon.go")
	}
	if _, err := os.Stat(filepath.Join(daemonDir, "start_alias.go")); !os.IsNotExist(err) {
		t.Errorf("root start alias must live at cmd/start.go, not cmd/daemon/start_alias.go")
	}

	for _, name := range []string{
		"daemon_start.go",
		"daemon_kill.go",
		"daemon_stop.go",
		"daemon_status.go",
	} {
		if _, err := os.Stat(filepath.Join("cmd", name)); !os.IsNotExist(err) {
			t.Errorf("legacy daemon command file cmd/%s still exists", name)
		}
	}
}

func TestWizardCommandsLayout(t *testing.T) {
	if _, err := os.Stat(filepath.Join("cmd", "wizard.go")); err != nil {
		t.Errorf("first-layer wizard command missing cmd/wizard.go: %v", err)
	}
	if _, err := os.Stat(filepath.Join("cmd", "wizard_test.go")); err != nil {
		t.Errorf("wizard CLI tests missing cmd/wizard_test.go: %v", err)
	}

	wizardDir := filepath.Join("cmd", "wizard")
	pkg, err := build.Default.ImportDir(wizardDir, build.ImportComment)
	if err != nil {
		t.Fatalf("load wizard command package: %v", err)
	}
	if pkg.Name != "wizard" {
		t.Fatalf("wizard command package name = %q, want wizard", pkg.Name)
	}

	for _, name := range []string{
		"install.go",
		"install_flags.go",
		"wizard_test.go",
	} {
		if _, err := os.Stat(filepath.Join(wizardDir, name)); err != nil {
			t.Errorf("wizard subcommand package missing %s: %v", name, err)
		}
	}

	if _, err := os.Stat(filepath.Join(wizardDir, "wizard.go")); !os.IsNotExist(err) {
		t.Errorf("parent command must live at cmd/wizard.go, not cmd/wizard/wizard.go")
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

func TestTaskmanagerCommandsLayout(t *testing.T) {
	if _, err := os.Stat(filepath.Join("cmd", "taskmanager.go")); err != nil {
		t.Errorf("first-layer taskmanager command missing cmd/taskmanager.go: %v", err)
	}

	dir := filepath.Join("cmd", "taskmanager")
	pkg, err := build.Default.ImportDir(dir, build.ImportComment)
	if err != nil {
		t.Fatalf("load taskmanager package: %v", err)
	}
	if pkg.Name != "taskmanager" {
		t.Fatalf("taskmanager package name = %q, want taskmanager", pkg.Name)
	}
	for _, name := range []string{"emit.go", "emit_text.go"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("taskmanager subcommand package missing %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "taskmanager.go")); !os.IsNotExist(err) {
		t.Errorf("parent command must live at cmd/taskmanager.go, not cmd/taskmanager/taskmanager.go")
	}
}

func TestLogsCommandsLayout(t *testing.T) {
	if _, err := os.Stat(filepath.Join("cmd", "logs.go")); err != nil {
		t.Errorf("first-layer logs command missing cmd/logs.go: %v", err)
	}

	dir := filepath.Join("cmd", "logs")
	pkg, err := build.Default.ImportDir(dir, build.ImportComment)
	if err != nil {
		t.Fatalf("load logs package: %v", err)
	}
	if pkg.Name != "logs" {
		t.Fatalf("logs package name = %q, want logs", pkg.Name)
	}
	if _, err := os.Stat(filepath.Join(dir, "monitor.go")); err != nil {
		t.Errorf("logs subcommand package missing monitor.go: %v", err)
	}
	if _, err := os.Stat(filepath.Join("cmd", "logs_monitor.go")); !os.IsNotExist(err) {
		t.Errorf("legacy logs monitor file cmd/logs_monitor.go still exists")
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
