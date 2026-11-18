package task

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bizshuk/pm2/process"
)

func fixtureApps() []process.AppConfig {
	return []process.AppConfig{
		{Namespace: "default", Name: "daily-report", Script: "./report.sh"},
		{Namespace: "default", Name: "planner", Script: "./planner.sh", Optional: true},
		{Namespace: "infra", Name: "auditor", Script: "./audit.sh", Optional: true},
	}
}

func names(apps []process.AppConfig) []string {
	out := make([]string, 0, len(apps))
	for _, a := range apps {
		out = append(out, a.Name)
	}
	return out
}

func TestSelectAppsDefaultsToRegisterOptionalAsPaused(t *testing.T) {
	selected, paused, err := selectApps(fixtureApps(), false, nil, registeredTasks{})
	if err != nil {
		t.Fatalf("selectApps: %v", err)
	}
	if len(selected) != 3 {
		t.Fatalf("selected = %q, want all apps registered", strings.Join(names(selected), ","))
	}
	if got := strings.Join(names(paused), ","); got != "planner,auditor" {
		t.Errorf("paused = %q, want %q", got, "planner,auditor")
	}
	if selected[0].Paused {
		t.Error("required app should start active")
	}
	if !selected[1].Paused || !selected[2].Paused {
		t.Errorf("optional apps should register paused: %+v", selected)
	}
}

func TestSelectAppsAllIncludesOptional(t *testing.T) {
	selected, paused, err := selectApps(fixtureApps(), true, nil, registeredTasks{})
	if err != nil {
		t.Fatalf("selectApps: %v", err)
	}
	if len(selected) != 3 {
		t.Errorf("selected = %v, want all 3", names(selected))
	}
	if len(paused) != 0 {
		t.Errorf("paused = %v, want none", names(paused))
	}
	for _, app := range selected {
		if app.Paused {
			t.Errorf("--all left %q paused", app.Name)
		}
	}
}

func TestSelectAppsWithOptsInByName(t *testing.T) {
	selected, paused, err := selectApps(fixtureApps(), false, []string{"planner"}, registeredTasks{})
	if err != nil {
		t.Fatalf("selectApps: %v", err)
	}
	if len(selected) != 3 {
		t.Fatalf("selected = %q, want all apps registered", strings.Join(names(selected), ","))
	}
	if got := strings.Join(names(paused), ","); got != "auditor" {
		t.Errorf("paused = %q, want %q", got, "auditor")
	}
	if selected[1].Paused {
		t.Error("--with planner should start planner active")
	}
	if !selected[2].Paused {
		t.Error("unnamed optional auditor should register paused")
	}
}

func TestSelectAppsWithAcceptsNamespacedKey(t *testing.T) {
	selected, paused, err := selectApps(fixtureApps(), false, []string{"infra:auditor"}, registeredTasks{})
	if err != nil {
		t.Fatalf("selectApps: %v", err)
	}
	if len(selected) != 3 {
		t.Fatalf("selected = %q, want all apps registered", strings.Join(names(selected), ","))
	}
	if got := strings.Join(names(paused), ","); got != "planner" {
		t.Errorf("paused = %q, want planner", got)
	}
	if selected[2].Paused {
		t.Error("--with infra:auditor should start auditor active")
	}
}

// Naming a required app is redundant but must not be an error — it is
// already selected, so --with is simply a no-op for it.
func TestSelectAppsWithRequiredAppIsNoOp(t *testing.T) {
	selected, paused, err := selectApps(fixtureApps(), false, []string{"daily-report"}, registeredTasks{})
	if err != nil {
		t.Fatalf("selectApps: %v", err)
	}
	if got := strings.Join(names(selected), ","); got != "daily-report,planner,auditor" {
		t.Errorf("selected = %q, want all apps registered", got)
	}
	if len(paused) != 2 {
		t.Errorf("paused = %v, want 2", names(paused))
	}
}

func TestSelectAppsWithUnknownNameErrors(t *testing.T) {
	_, _, err := selectApps(fixtureApps(), false, []string{"plannr"}, registeredTasks{})
	if err == nil {
		t.Fatal("expected an error for an unknown --with name")
	}
	if !strings.Contains(err.Error(), "plannr") {
		t.Errorf("error %q should name the offending value", err)
	}
}

// A config with no optional apps must behave exactly as before the flag
// existed: everything starts, nothing is skipped.
func TestSelectAppsNoOptionalIsUnchanged(t *testing.T) {
	apps := []process.AppConfig{
		{Name: "api", Script: "./api.sh"},
		{Name: "worker", Script: "./worker.sh"},
	}
	selected, paused, err := selectApps(apps, false, nil, registeredTasks{})
	if err != nil {
		t.Fatalf("selectApps: %v", err)
	}
	if len(selected) != 2 || len(paused) != 0 {
		t.Errorf("selected = %v, paused = %v", names(selected), names(paused))
	}
}

func TestSelectSingleAppByNumberActivatesOptionalApp(t *testing.T) {
	app, err := selectSingleApp(fixtureApps(), "2")
	if err != nil {
		t.Fatalf("selectSingleApp: %v", err)
	}
	if app.Name != "planner" {
		t.Fatalf("selected app = %q, want planner", app.Name)
	}
	if app.Paused {
		t.Error("an explicitly selected optional app must be active")
	}
}

func TestSelectSingleAppByNamespacedKey(t *testing.T) {
	app, err := selectSingleApp(fixtureApps(), "infra:auditor")
	if err != nil {
		t.Fatalf("selectSingleApp: %v", err)
	}
	if app.Name != "auditor" {
		t.Fatalf("selected app = %q, want auditor", app.Name)
	}
}

func TestSelectSingleAppRejectsAmbiguousName(t *testing.T) {
	apps := []process.AppConfig{
		{Namespace: "one", Name: "worker", Script: "./one.sh"},
		{Namespace: "two", Name: "worker", Script: "./two.sh"},
	}

	_, err := selectSingleApp(apps, "worker")
	if err == nil {
		t.Fatal("expected an ambiguous bare name to fail")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error = %q, want ambiguity explanation", err)
	}
}

func TestSelectSingleAppRejectsInvalidChoice(t *testing.T) {
	_, err := selectSingleApp(fixtureApps(), "99")
	if err == nil {
		t.Fatal("expected an out-of-range choice to fail")
	}
	if !strings.Contains(err.Error(), "99") {
		t.Errorf("error = %q, want invalid choice", err)
	}
}

func TestChooseSingleAppListsAndReturnsOnlyChosenApp(t *testing.T) {
	var out bytes.Buffer
	app, err := chooseSingleApp(fixtureApps(), strings.NewReader("3\n"), &out)
	if err != nil {
		t.Fatalf("chooseSingleApp: %v", err)
	}
	if app.Name != "auditor" {
		t.Fatalf("selected app = %q, want auditor", app.Name)
	}
	for _, want := range []string{"1) default:daily-report", "2) default:planner", "3) infra:auditor", "Choose one app"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("prompt output %q does not contain %q", out.String(), want)
		}
	}
}

func TestChooseSingleAppRejectsEmptyEcosystem(t *testing.T) {
	var out bytes.Buffer
	_, err := chooseSingleApp(nil, strings.NewReader(""), &out)
	if err == nil {
		t.Fatal("expected an empty ecosystem to fail")
	}
	if !strings.Contains(err.Error(), "no apps") {
		t.Errorf("error = %q, want no apps explanation", err)
	}
}

func registeredFixture(entries map[string]process.Status) registeredTasks {
	infos := make([]process.ProcessInfo, 0, len(entries))
	for key, status := range entries {
		namespace, name, _ := strings.Cut(key, ":")
		infos = append(infos, process.ProcessInfo{
			AppConfig: process.AppConfig{Namespace: namespace, Name: name},
			Status:    status,
		})
	}
	return newRegisteredTasks(infos)
}

// Re-applying a file must not undo `pm2 task pause` / `resume`: an existing
// task keeps its paused state whatever its `optional` field says.
func TestSelectAppsKeepsExistingPausedState(t *testing.T) {
	registered := registeredFixture(map[string]process.Status{
		"default:daily-report": process.StatusPaused, // required, user paused it
		"default:planner":      process.StatusOnline, // optional, user resumed it
	})
	selected, paused, err := selectApps(fixtureApps(), false, nil, registered)
	if err != nil {
		t.Fatalf("selectApps: %v", err)
	}
	if !selected[0].Paused {
		t.Error("existing paused required app should stay paused")
	}
	if selected[1].Paused {
		t.Error("existing active optional app should stay active")
	}
	if !selected[2].Paused {
		t.Error("new optional app should register paused")
	}
	if got := strings.Join(names(paused), ","); got != "daily-report,auditor" {
		t.Errorf("paused = %q, want daily-report,auditor", got)
	}
}

// An app whose namespace changed in the file is still the task it
// replaces when the same file declares the same name.
func TestSelectAppsMatchesExistingByConfigFile(t *testing.T) {
	registered := newRegisteredTasks([]process.ProcessInfo{{
		AppConfig: process.AppConfig{Namespace: "old", Name: "daily-report", ConfigFile: "/eco.js"},
		Status:    process.StatusPaused,
	}})
	apps := []process.AppConfig{{Namespace: "new", Name: "daily-report", ConfigFile: "/eco.js"}}
	selected, _, err := selectApps(apps, false, nil, registered)
	if err != nil {
		t.Fatalf("selectApps: %v", err)
	}
	if !selected[0].Paused {
		t.Error("moved app should keep its paused state")
	}
}

// --with and --all describe how a *new* optional app registers; they never
// override the state a task already holds in the daemon. Resetting that
// state is --follow-optional's job, which passes no registered tasks.
func TestSelectAppsExistingStateWinsOverActivationFlags(t *testing.T) {
	registered := registeredFixture(map[string]process.Status{
		"default:daily-report": process.StatusPaused,
		"default:planner":      process.StatusPaused,
	})
	selected, _, err := selectApps(fixtureApps(), true, []string{"planner"}, registered)
	if err != nil {
		t.Fatalf("selectApps: %v", err)
	}
	if !selected[0].Paused || !selected[1].Paused {
		t.Error("existing paused apps should stay paused despite --all / --with")
	}
	if selected[2].Paused {
		t.Error("--all should start the new optional app")
	}
}

// With no registered tasks (--follow-optional) every app gets the
// optional policy, so a paused required task starts and a running
// optional one registers paused.
func TestSelectAppsWithoutRegisteredFollowsOptional(t *testing.T) {
	selected, _, err := selectApps(fixtureApps(), false, nil, registeredTasks{})
	if err != nil {
		t.Fatalf("selectApps: %v", err)
	}
	if selected[0].Paused || !selected[1].Paused || !selected[2].Paused {
		t.Errorf("want required active and optional paused, got %+v", selected)
	}
}

func TestRegisteredPausedStateAcrossInstances(t *testing.T) {
	app := process.AppConfig{Namespace: "default", Name: "api", Instances: 2}
	tests := []struct {
		name       string
		entries    map[string]process.Status
		wantPaused bool
		wantFound  bool
	}{
		{name: "none registered", entries: nil},
		{name: "all paused", entries: map[string]process.Status{
			"default:api-0": process.StatusPaused, "default:api-1": process.StatusPaused,
		}, wantPaused: true, wantFound: true},
		{name: "one running", entries: map[string]process.Status{
			"default:api-0": process.StatusPaused, "default:api-1": process.StatusOnline,
		}, wantFound: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paused, found := registeredFixture(tt.entries).pausedState(app)
			if paused != tt.wantPaused || found != tt.wantFound {
				t.Errorf("pausedState = (%v, %v), want (%v, %v)", paused, found, tt.wantPaused, tt.wantFound)
			}
		})
	}
}
