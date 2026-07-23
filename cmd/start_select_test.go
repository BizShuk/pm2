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

func TestSelectAppsDefaultsToRequiredOnly(t *testing.T) {
	selected, skipped, err := selectApps(fixtureApps(), false, nil)
	if err != nil {
		t.Fatalf("selectApps: %v", err)
	}
	if got := strings.Join(names(selected), ","); got != "daily-report" {
		t.Errorf("selected = %q, want %q", got, "daily-report")
	}
	if got := strings.Join(names(skipped), ","); got != "planner,auditor" {
		t.Errorf("skipped = %q, want %q", got, "planner,auditor")
	}
}

func TestSelectAppsAllIncludesOptional(t *testing.T) {
	selected, skipped, err := selectApps(fixtureApps(), true, nil)
	if err != nil {
		t.Fatalf("selectApps: %v", err)
	}
	if len(selected) != 3 {
		t.Errorf("selected = %v, want all 3", names(selected))
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none", names(skipped))
	}
}

func TestSelectAppsWithOptsInByName(t *testing.T) {
	selected, skipped, err := selectApps(fixtureApps(), false, []string{"planner"})
	if err != nil {
		t.Fatalf("selectApps: %v", err)
	}
	if got := strings.Join(names(selected), ","); got != "daily-report,planner" {
		t.Errorf("selected = %q, want %q", got, "daily-report,planner")
	}
	if got := strings.Join(names(skipped), ","); got != "auditor" {
		t.Errorf("skipped = %q, want %q", got, "auditor")
	}
}

func TestSelectAppsWithAcceptsNamespacedKey(t *testing.T) {
	selected, _, err := selectApps(fixtureApps(), false, []string{"infra:auditor"})
	if err != nil {
		t.Fatalf("selectApps: %v", err)
	}
	if got := strings.Join(names(selected), ","); got != "daily-report,auditor" {
		t.Errorf("selected = %q, want %q", got, "daily-report,auditor")
	}
}

// Naming a required app is redundant but must not be an error — it is
// already selected, so --with is simply a no-op for it.
func TestSelectAppsWithRequiredAppIsNoOp(t *testing.T) {
	selected, skipped, err := selectApps(fixtureApps(), false, []string{"daily-report"})
	if err != nil {
		t.Fatalf("selectApps: %v", err)
	}
	if got := strings.Join(names(selected), ","); got != "daily-report" {
		t.Errorf("selected = %q, want %q", got, "daily-report")
	}
	if len(skipped) != 2 {
		t.Errorf("skipped = %v, want 2", names(skipped))
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
	selected, skipped, err := selectApps(apps, false, nil)
	if err != nil {
		t.Fatalf("selectApps: %v", err)
	}
	if len(selected) != 2 || len(skipped) != 0 {
		t.Errorf("selected = %v, skipped = %v", names(selected), names(skipped))
	}
}
