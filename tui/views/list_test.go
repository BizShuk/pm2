package views

import (
	"strings"
	"testing"

	"github.com/bizshuk/pm2/process"
)

func TestRenderProcessTableIsStaticAndNoColorSafe(t *testing.T) {
	out := RenderProcessTable([]process.ProcessInfo{{
		AppConfig: process.AppConfig{Name: "worker", Namespace: "jobs"},
		ID:        7,
		Status:    process.StatusPaused,
	}}, ProcessTableOptions{Width: 160, NoColor: true})

	for _, want := range []string{"┌", "worker", "jobs", "paused", "└"} {
		if !strings.Contains(out, want) {
			t.Errorf("process table missing %q: %q", want, out)
		}
	}
	for _, interactiveChrome := range []string{"pm2 monitor", "navigate", "cpu: ", "net: "} {
		if strings.Contains(out, interactiveChrome) {
			t.Errorf("static process table contains interactive chrome %q: %q", interactiveChrome, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("NoColor process table contains ANSI escapes: %q", out)
	}
}

func TestProcessTableColumnsKeepCoreFieldsOnNarrowTerminal(t *testing.T) {
	cols := processTableColumns(70)
	present := make(map[string]bool, len(cols))
	for _, col := range cols {
		present[col.name] = true
	}

	for _, want := range []string{"id", "namespace", "name", "pid", "uptime", "↺", "status", "cpu", "mem"} {
		if !present[want] {
			t.Errorf("narrow table dropped core column %q", want)
		}
	}
	for _, dropped := range []string{"version", "user", "cron", "last exec"} {
		if present[dropped] {
			t.Errorf("narrow table retained optional column %q", dropped)
		}
	}
}
