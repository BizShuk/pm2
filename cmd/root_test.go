package cmd

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	sdkcmd "github.com/bizshuk/gosdk/cmd"
	sdkconfig "github.com/bizshuk/gosdk/config"
	"github.com/bizshuk/pm2/model"
)

func TestRootCmdRegistersConfigCmd(t *testing.T) {
	command, _, err := Cmd.Find([]string{"config"})
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
	startCmd, _, err := Cmd.Find([]string{"start"})
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

	daemonStartCmd, _, err := Cmd.Find([]string{"daemon", "start"})
	if err != nil {
		t.Fatalf("find daemon start command: %v", err)
	}
	if startCmd.RunE == nil || daemonStartCmd.RunE == nil {
		t.Fatal("start aliases must use RunE")
	}
	if reflect.ValueOf(startCmd.RunE).Pointer() != reflect.ValueOf(daemonStartCmd.RunE).Pointer() {
		t.Error("pm2 start and pm2 daemon start must share the same handler")
	}

	applyCmd, _, err := Cmd.Find([]string{"apply"})
	if err != nil {
		t.Fatalf("find apply command: %v", err)
	}
	taskStartCmd, _, err := Cmd.Find([]string{"task", "start"})
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
	for _, command := range Cmd.Commands() {
		rootCommands[command.Name()] = true
	}
	for _, name := range []string{"run", "restart", "stop", "pause", "resume", "delete"} {
		if rootCommands[name] {
			t.Errorf("implicit task alias %q must not be registered at the root", name)
		}
	}
}

func TestRootCmdShortAliases(t *testing.T) {
	for commandName, alias := range map[string]string{
		"wizard":      "w",
		"save":        "s",
		"resurrect":   "r",
		"task":        "t",
		"daemon":      "d",
		"monitor":     "m",
		"list":        "l",
		"taskmanager": "tm",
	} {
		t.Run(commandName, func(t *testing.T) {
			command, _, err := Cmd.Find([]string{commandName})
			if err != nil {
				t.Fatalf("find %s command: %v", commandName, err)
			}
			aliasCommand, _, err := Cmd.Find([]string{alias})
			if err != nil {
				t.Fatalf("find %s alias: %v", alias, err)
			}
			if aliasCommand != command {
				t.Fatalf("pm2 %s resolves to %q, want %q", alias, aliasCommand.Name(), commandName)
			}

			wantDescription := "short alias: pm2 " + alias
			if !strings.Contains(strings.ToLower(command.Short), wantDescription) {
				t.Errorf("%s description = %q, want %q", commandName, command.Short, wantDescription)
			}
		})
	}
}

func TestRootCmdTaskSubcommands(t *testing.T) {
	for _, taskCommand := range []string{"start", "restart", "stop", "pause", "resume", "delete"} {
		t.Run(taskCommand, func(t *testing.T) {
			nested, _, err := Cmd.Find([]string{"task", taskCommand})
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

	taskStart, _, err := Cmd.Find([]string{"task", "start"})
	if err != nil {
		t.Fatalf("find task start: %v", err)
	}
	for _, flagName := range []string{"all", "with", "single"} {
		if taskStart.Flags().Lookup(flagName) == nil {
			t.Errorf("task start missing --%s flag", flagName)
		}
	}
}

func TestExecutePrintsVersionAliases(t *testing.T) {
	for _, arg := range []string{"version", "-v", "--v", "--version", "-version"} {
		t.Run(arg, func(t *testing.T) {
			var out bytes.Buffer
			Cmd.SetOut(&out)
			t.Cleanup(func() {
				Cmd.SetOut(nil)
				Cmd.SetArgs(nil)
			})

			if err := Execute([]string{arg}); err != nil {
				t.Fatalf("Execute(%q): %v", arg, err)
			}
			if got, want := out.String(), model.PM2Version+"\n"; got != want {
				t.Fatalf("version output = %q, want %q", got, want)
			}
		})
	}
}
