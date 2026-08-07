package wizard

import (
	"testing"

	plannerprompt "github.com/bizshuk/pm2/cmd/wizard/prompt"
)

// This file holds package-local unit tests for install helpers.
// CLI-level wizard integration tests live in cmd/wizard_test.go.

func TestPlannerFlagsUseTemplates(t *testing.T) {
	for _, template := range []plannerprompt.Template{
		plannerprompt.System(),
		plannerprompt.Business(),
	} {
		flag := InstallCmd.Flags().Lookup(template.Flag)
		if flag == nil {
			t.Fatalf("missing --%s flag", template.Flag)
		}
		if flag.Usage != template.Help {
			t.Errorf("--%s help = %q, want %q", template.Flag, flag.Usage, template.Help)
		}
	}
}

func TestBuildInstallApp(t *testing.T) {
	template := plannerprompt.System()
	app := buildInstallApp(
		"/abs/agy",
		template.Render("analyze repo"),
		EcoPlannerNS,
		"pm2",
		"/home/user/pm2",
	)
	if app.Script != "/abs/agy" {
		t.Errorf("Script = %q, want /abs/agy", app.Script)
	}
	if app.Name != "agy-pm2" {
		t.Errorf("Name = %q, want agy-pm2", app.Name)
	}
	if app.Namespace != EcoPlannerNS {
		t.Errorf("Namespace = %q, want %q", app.Namespace, EcoPlannerNS)
	}
	if app.CWD != "/home/user/pm2" {
		t.Errorf("CWD = %q, want /home/user/pm2", app.CWD)
	}
	// agy is a planner agent → --add-dir <cwd> prepended; prefix+prompt
	// joined into one single-quoted -p arg.
	wantArgs := []string{
		"--add-dir", "/home/user/pm2",
		"-p", "'" + template.Render("analyze repo") + "'",
	}
	if len(app.Args) != len(wantArgs) {
		t.Fatalf("len(Args) = %d, want %d", len(app.Args), len(wantArgs))
	}
	for i, a := range wantArgs {
		if app.Args[i] != a {
			t.Errorf("Args[%d] = %q, want %q", i, app.Args[i], a)
		}
	}
	if app.Instances != 1 {
		t.Errorf("Instances = %d, want 1", app.Instances)
	}
}

func TestBuildInstallAppEmptyUserPrompt(t *testing.T) {
	template := plannerprompt.Business()
	app := buildInstallApp(
		"/abs/agy",
		template.Render(""),
		EcoPlannerNS,
		"myproj",
		"/home/user/proj",
	)
	// Empty user_prompt → prompt is just the prefix, still single-quoted.
	wantArgs := []string{
		"--add-dir", "/home/user/proj",
		"-p", "'" + template.Render("") + "'",
	}
	if len(app.Args) != len(wantArgs) {
		t.Fatalf("len(Args) = %d, want %d", len(app.Args), len(wantArgs))
	}
	for i, a := range wantArgs {
		if app.Args[i] != a {
			t.Errorf("Args[%d] = %q, want %q", i, app.Args[i], a)
		}
	}
	if app.Name != "agy-myproj" {
		t.Errorf("Name = %q, want agy-myproj", app.Name)
	}
	if app.Namespace != EcoPlannerNS {
		t.Errorf("Namespace = %q, want %q", app.Namespace, EcoPlannerNS)
	}
}

// buildInstallApp should drop the cwd suffix entirely when cwdBasename
// is empty (defensive guard for unusual Getwd failures).
func TestBuildInstallAppEmptyCwdBasename(t *testing.T) {
	app := buildInstallApp(
		"/abs/agy",
		plannerprompt.System().Render("x"),
		EcoPlannerNS,
		"",
		"/abs/cwd",
	)
	if app.Name != "agy" {
		t.Errorf("Name = %q, want agy (no suffix when cwdBasename empty)", app.Name)
	}
}

func TestIsPlannerAgent(t *testing.T) {
	tests := []struct {
		script string
		want   bool
	}{
		{"agy", true},
		{"claude", true},
		{"claudem", true},
		{"claudew", true},
		{"/usr/local/bin/claudem", true},
		{"/usr/local/bin/claudew", true},
		{"node", false},
		{"python", false},
	}
	for _, tc := range tests {
		if got := isPlannerAgent(tc.script); got != tc.want {
			t.Errorf("isPlannerAgent(%q) = %t, want %t", tc.script, got, tc.want)
		}
	}
}
