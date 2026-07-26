package cmd

import (
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
	selected, paused, err := selectApps(fixtureApps(), false, nil)
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
	selected, paused, err := selectApps(fixtureApps(), true, nil)
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
	selected, paused, err := selectApps(fixtureApps(), false, []string{"planner"})
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
	selected, paused, err := selectApps(fixtureApps(), false, []string{"infra:auditor"})
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
	selected, paused, err := selectApps(fixtureApps(), false, []string{"daily-report"})
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
	_, _, err := selectApps(fixtureApps(), false, []string{"plannr"})
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
	selected, paused, err := selectApps(apps, false, nil)
	if err != nil {
		t.Fatalf("selectApps: %v", err)
	}
	if len(selected) != 2 || len(paused) != 0 {
		t.Errorf("selected = %v, paused = %v", names(selected), names(paused))
	}
}
