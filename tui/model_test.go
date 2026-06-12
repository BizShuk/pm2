package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/shuk/pm2/process"
)

func TestBuildDetailScriptArgsCombined(t *testing.T) {
	m := New("mock_socket", true)
	p := process.ProcessInfo{
		ID:        1,
		Name:      "test-app",
		Script:    "/path/to/script.sh",
		Args:      []string{"--foo", "bar", "-v"},
		Status:    process.StatusOnline,
		StartedAt: time.Now(),
	}

	detail := m.buildDetail(p, 100)

	expected := "/path/to/script.sh --foo bar -v"
	if !strings.Contains(detail, expected) {
		t.Errorf("Expected detail to contain %q, but got %q", expected, detail)
	}
}

func TestBuildDetailScriptNoArgs(t *testing.T) {
	m := New("mock_socket", true)
	p := process.ProcessInfo{
		ID:        1,
		Name:      "test-app",
		Script:    "/path/to/script.sh",
		Args:      nil,
		Status:    process.StatusOnline,
		StartedAt: time.Now(),
	}

	detail := m.buildDetail(p, 100)

	expected := "/path/to/script.sh"
	if !strings.Contains(detail, expected) {
		t.Errorf("Expected detail to contain %q, but got %q", expected, detail)
	}
}
