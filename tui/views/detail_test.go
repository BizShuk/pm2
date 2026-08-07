package views

import (
	"strings"
	"testing"
	"unicode"

	"github.com/bizshuk/pm2/process"
)

func TestRenderDetailWrapsScriptToPaneWidth(t *testing.T) {
	processInfo := process.ProcessInfo{
		AppConfig: process.AppConfig{
			Name:   "reporter",
			Script: "/srv/automation/reporting/run-daily-report.sh",
			Args:   []string{"--workspace", "/srv/customer-data/singapore", "--format", "long-form"},
		},
	}

	narrow := plain(RenderDetail(processInfo, 40))
	wide := plain(RenderDetail(processInfo, 70))
	narrowScript := detailFieldLines(narrow, "script", "cwd")
	wideScript := detailFieldLines(wide, "script", "cwd")

	if len(narrowScript) <= 1 {
		t.Fatalf("narrow script used %d line, want wrapped output:\n%s", len(narrowScript), narrow)
	}
	if len(narrowScript) <= len(wideScript) {
		t.Fatalf("narrow script used %d lines, wide used %d; want terminal width to control wrapping", len(narrowScript), len(wideScript))
	}
	compactScript := compactWhitespace(strings.Join(narrowScript, "\n"))
	for _, want := range []string{"run-daily-report.sh", "--workspace", "/srv/customer-data/singapore", "--format", "long-form"} {
		if !strings.Contains(compactScript, compactWhitespace(want)) {
			t.Errorf("wrapped script is missing %q:\n%s", want, strings.Join(narrowScript, "\n"))
		}
	}
	for index, line := range strings.Split(narrow, "\n") {
		if width := screen.StringWidth(line); width > 40 {
			t.Errorf("line %d is %d columns, want at most 40: %q", index, width, line)
		}
	}
}

func compactWhitespace(value string) string {
	return strings.Map(func(char rune) rune {
		if unicode.IsSpace(char) {
			return -1
		}
		return char
	}, value)
}

func detailFieldLines(rendered, startLabel, nextLabel string) []string {
	lines := strings.Split(rendered, "\n")
	start := -1
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if start < 0 && strings.HasPrefix(trimmed, startLabel) {
			start = index
			continue
		}
		if start >= 0 && strings.HasPrefix(trimmed, nextLabel) {
			return lines[start:index]
		}
	}
	if start >= 0 {
		return lines[start:]
	}
	return nil
}
