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

func TestProcessSorting(t *testing.T) {
	m := New("mock_socket", false)

	p1 := process.ProcessInfo{
		ID:        1,
		Name:      "b-app",
		Namespace: "prod",
		CPU:       5.5,
		Memory:    2048,
		Status:    process.StatusOnline,
	}
	p2 := process.ProcessInfo{
		ID:        2,
		Name:      "a-app",
		Namespace: "dev",
		CPU:       10.2,
		Memory:    1024,
		Status:    process.StatusErrored,
	}
	p3 := process.ProcessInfo{
		ID:        3,
		Name:      "c-app",
		Namespace: "dev",
		CPU:       2.1,
		Memory:    4096,
		Status:    process.StatusStopped,
	}

	procs := []process.ProcessInfo{p1, p2, p3}

	// 1. Sort by name
	m.procs = make([]process.ProcessInfo, len(procs))
	copy(m.procs, procs)
	m.SortBy = SortByName
	m.sortProcs()
	if m.procs[0].Name != "a-app" || m.procs[1].Name != "b-app" || m.procs[2].Name != "c-app" {
		t.Errorf("Expected sorted by name: a-app, b-app, c-app. Got: %s, %s, %s",
			m.procs[0].Name, m.procs[1].Name, m.procs[2].Name)
	}

	// 2. Sort by namespace
	m.procs = make([]process.ProcessInfo, len(procs))
	copy(m.procs, procs)
	m.SortBy = SortByNamespace
	m.sortProcs()
	if m.procs[0].Namespace != "dev" || m.procs[1].Namespace != "dev" || m.procs[2].Namespace != "prod" {
		t.Errorf("Expected sorted by namespace order. Got: %s, %s, %s",
			m.procs[0].Namespace, m.procs[1].Namespace, m.procs[2].Namespace)
	}
	if m.procs[0].Name != "a-app" || m.procs[1].Name != "c-app" {
		t.Errorf("Expected same namespace sorted by name. Got: %s, %s",
			m.procs[0].Name, m.procs[1].Name)
	}

	// 3. Sort by CPU
	m.procs = make([]process.ProcessInfo, len(procs))
	copy(m.procs, procs)
	m.SortBy = SortByCPU
	m.sortProcs()
	if m.procs[0].Name != "a-app" || m.procs[1].Name != "b-app" || m.procs[2].Name != "c-app" {
		t.Errorf("Expected sorted by CPU (descending). Got: %s, %s, %s",
			m.procs[0].Name, m.procs[1].Name, m.procs[2].Name)
	}

	// 4. Sort by Memory
	m.procs = make([]process.ProcessInfo, len(procs))
	copy(m.procs, procs)
	m.SortBy = SortByMem
	m.sortProcs()
	if m.procs[0].Name != "c-app" || m.procs[1].Name != "b-app" || m.procs[2].Name != "a-app" {
		t.Errorf("Expected sorted by Memory (descending). Got: %s, %s, %s",
			m.procs[0].Name, m.procs[1].Name, m.procs[2].Name)
	}

	// 5. Sort by Status
	m.procs = make([]process.ProcessInfo, len(procs))
	copy(m.procs, procs)
	m.SortBy = SortByStatus
	m.sortProcs()
	if m.procs[0].Name != "a-app" || m.procs[1].Name != "b-app" || m.procs[2].Name != "c-app" {
		t.Errorf("Expected sorted by Status. Got: %s, %s, %s",
			m.procs[0].Name, m.procs[1].Name, m.procs[2].Name)
	}
}
