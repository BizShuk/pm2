package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bizshuk/pm2/process"
)

func TestRenderListUsesBorderedProcessTable(t *testing.T) {
	t.Setenv("COLUMNS", "160")

	infos := []process.ProcessInfo{{
		AppConfig: process.AppConfig{
			Name:      "api",
			Namespace: "prod",
			Version:   "1.2.3",
		},
		ID:     1,
		PID:    4321,
		Status: process.StatusOnline,
		CPU:    2.5,
		Memory: 4 * 1024 * 1024,
	}}

	var out bytes.Buffer
	renderList(&out, infos, &listOptions{noColor: true})
	got := out.String()

	for _, want := range []string{"┌", "┬", "├", "┼", "└", "┴", "api", "prod", "online"} {
		if !strings.Contains(got, want) {
			t.Errorf("styled list output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("--no-color output contains ANSI escapes: %q", got)
	}
}
