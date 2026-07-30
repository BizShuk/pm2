package cmd

import (
	"testing"
)

func TestMonitorCommandName(t *testing.T) {
	if got := MonitorCmd.Name(); got != "monitor" {
		t.Fatalf("MonitorCmd.Name() = %q, want %q", got, "monitor")
	}
	hasShortAlias := false
	for _, alias := range MonitorCmd.Aliases {
		if alias == "monit" {
			t.Fatal("MonitorCmd still exposes the legacy monit alias")
		}
		if alias == "m" {
			hasShortAlias = true
		}
	}
	if !hasShortAlias {
		t.Fatalf("MonitorCmd.Aliases = %v, want m", MonitorCmd.Aliases)
	}

	command, _, err := Cmd.Find([]string{"m"})
	if err != nil {
		t.Fatalf("find pm2 m: %v", err)
	}
	if command != MonitorCmd {
		t.Fatalf("pm2 m resolves to %q, want MonitorCmd", command.Name())
	}
}

func TestMonitorDefaultsToDetailWithoutDetailFlag(t *testing.T) {
	m := newMonitorModel("/tmp/pm2-test.sock")
	if !m.Detail {
		t.Fatal("pm2 monitor model should default to the detail dashboard")
	}
	if flag := MonitorCmd.Flags().Lookup("detail"); flag != nil {
		t.Fatalf("pm2 monitor still exposes --detail: %#v", flag)
	}
	if flag := MonitorCmd.Flags().ShorthandLookup("d"); flag != nil {
		t.Fatalf("pm2 monitor still exposes -d: %#v", flag)
	}
}

func TestMonitorDoesNotOwnLogExplorer(t *testing.T) {
	for _, command := range MonitorCmd.Commands() {
		if command.Name() == "logs" {
			t.Fatal("MonitorCmd still owns the Interactive Log Explorer")
		}
	}
}
