package main

import (
	"go/build"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	sdkcmd "github.com/bizshuk/gosdk/cmd"
	sdkconfig "github.com/bizshuk/gosdk/config"
)

func TestRootCmdRegistersConfigCmd(t *testing.T) {
	command, _, err := RootCmd.Find([]string{"config"})
	if err != nil {
		t.Fatalf("find config command: %v", err)
	}
	if command != sdkcmd.ConfigCmd {
		t.Fatalf("config command = %p, want gosdk ConfigCmd %p", command, sdkcmd.ConfigCmd)
	}
}

func TestRootCmdInitializesAppConfigDir(t *testing.T) {
	if got := sdkconfig.GetAppName(); got != "pm2" {
		t.Fatalf("app name = %q, want pm2", got)
	}
	dir := sdkconfig.GetAppConfigDir()
	if dir == "" || filepath.Base(dir) != "pm2" {
		t.Fatalf("app config dir = %q, want ~/.config/pm2", dir)
	}
}

func TestRootCmdCommandNamespaces(t *testing.T) {
	startCmd, _, err := RootCmd.Find([]string{"start"})
	if err != nil {
		t.Fatalf("find start command: %v", err)
	}
	if !strings.Contains(strings.ToLower(startCmd.Short), "daemon") {
		t.Errorf("start description = %q, want daemon startup description", startCmd.Short)
	}
	if startCmd.Flags().Lookup("all") != nil || startCmd.Flags().Lookup("with") != nil {
		t.Error("top-level start must not expose task selection flags")
	}
	if flag := startCmd.Flags().Lookup("foreground"); flag == nil || flag.Shorthand != "f" {
		t.Error("top-level start missing -f, --foreground")
	}

	daemonStartCmd, _, err := RootCmd.Find([]string{"daemon", "start"})
	if err != nil {
		t.Fatalf("find daemon start command: %v", err)
	}
	if startCmd.RunE == nil || daemonStartCmd.RunE == nil {
		t.Fatal("start aliases must use RunE")
	}
	if reflect.ValueOf(startCmd.RunE).Pointer() != reflect.ValueOf(daemonStartCmd.RunE).Pointer() {
		t.Error("pm2 start and pm2 daemon start must share the same handler")
	}

	applyCmd, _, err := RootCmd.Find([]string{"apply"})
	if err != nil {
		t.Fatalf("find apply command: %v", err)
	}
	taskStartCmd, _, err := RootCmd.Find([]string{"task", "start"})
	if err != nil {
		t.Fatalf("find task start command: %v", err)
	}
	if applyCmd.RunE == nil || taskStartCmd.RunE == nil {
		t.Fatal("apply aliases must use RunE")
	}
	if reflect.ValueOf(applyCmd.RunE).Pointer() != reflect.ValueOf(taskStartCmd.RunE).Pointer() {
		t.Error("pm2 apply and pm2 task start must share the same handler")
	}
	if !strings.Contains(strings.ToLower(taskStartCmd.Short), "short alias: pm2 apply") {
		t.Errorf("task start description = %q, want pm2 apply short alias", taskStartCmd.Short)
	}
	if !strings.Contains(taskStartCmd.Example, "pm2 apply") {
		t.Errorf("task start usage examples do not mention pm2 apply: %q", taskStartCmd.Example)
	}
	if !strings.Contains(applyCmd.Example, "pm2 task start") {
		t.Errorf("apply usage examples do not mention pm2 task start: %q", applyCmd.Example)
	}
	if !strings.Contains(applyCmd.Long, "ecosystem.config.js") ||
		!strings.Contains(strings.ToLower(applyCmd.Long), "current directory") {
		t.Errorf("apply long help must describe the current ecosystem.config.js default: %q", applyCmd.Long)
	}
	for _, flagName := range []string{"all", "with", "single"} {
		if applyCmd.Flags().Lookup(flagName) == nil {
			t.Errorf("apply command missing --%s flag", flagName)
		}
	}

	rootCommands := make(map[string]bool)
	for _, command := range RootCmd.Commands() {
		rootCommands[command.Name()] = true
	}
	for _, name := range []string{"run", "restart", "stop", "pause", "resume", "delete"} {
		if rootCommands[name] {
			t.Errorf("implicit task alias %q must not be registered at the root", name)
		}
	}
}

func TestRootCmdTaskSubcommands(t *testing.T) {
	for _, taskCommand := range []string{"start", "restart", "stop", "pause", "resume", "delete"} {
		t.Run(taskCommand, func(t *testing.T) {
			nested, _, err := RootCmd.Find([]string{"task", taskCommand})
			if err != nil {
				t.Fatalf("find task %s: %v", taskCommand, err)
			}
			if nested.Name() != taskCommand {
				t.Fatalf("task command name = %q, want %q", nested.Name(), taskCommand)
			}
			if nested.RunE == nil {
				t.Fatal("task command must use RunE")
			}
			if taskCommand != "start" && strings.Contains(strings.ToLower(nested.Short), "alias") {
				t.Errorf("task %s advertises an implicit root alias: %q", taskCommand, nested.Short)
			}
			if taskCommand == "restart" && !strings.Contains(strings.ToLower(nested.Short), "task") {
				t.Errorf("task restart description = %q, want task vocabulary", nested.Short)
			}
		})
	}

	taskStart, _, err := RootCmd.Find([]string{"task", "start"})
	if err != nil {
		t.Fatalf("find task start: %v", err)
	}
	for _, flagName := range []string{"all", "with", "single"} {
		if taskStart.Flags().Lookup(flagName) == nil {
			t.Errorf("task start missing --%s flag", flagName)
		}
	}
}

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
