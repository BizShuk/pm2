package cmd

import "testing"

func TestMonitDefaultsToDetailWithoutDetailFlag(t *testing.T) {
	m := newMonitModel("/tmp/pm2-test.sock")
	if !m.Detail {
		t.Fatal("pm2 monit model should default to the detail dashboard")
	}
	if flag := MonitCmd.Flags().Lookup("detail"); flag != nil {
		t.Fatalf("pm2 monit still exposes --detail: %#v", flag)
	}
	if flag := MonitCmd.Flags().ShorthandLookup("d"); flag != nil {
		t.Fatalf("pm2 monit still exposes -d: %#v", flag)
	}
}
