package cmd

import "testing"

func TestMonitorCommandName(t *testing.T) {
	if got := MonitorCmd.Name(); got != "monitor" {
		t.Fatalf("MonitorCmd.Name() = %q, want %q", got, "monitor")
	}
	for _, alias := range MonitorCmd.Aliases {
		if alias == "monit" {
			t.Fatal("MonitorCmd still exposes the legacy monit alias")
		}
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
