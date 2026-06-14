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

func TestRefreshPreservesSelection(t *testing.T) {
	m := New("mock_socket", false)
	m.SortBy = SortByName

	// Setup initial processes (sorted by Name: a-app [ID: 2], b-app [ID: 1])
	m.procs = []process.ProcessInfo{
		{ID: 2, Name: "a-app"},
		{ID: 1, Name: "b-app"},
	}
	m.selected = 0 // points to "a-app" (ID 2)

	// Simulate refreshMsg, which receives a list sorted by ID (b-app [ID: 1], a-app [ID: 2])
	refreshedProcs := []process.ProcessInfo{
		{ID: 1, Name: "b-app"},
		{ID: 2, Name: "a-app"},
	}

	msg := refreshMsg{procs: refreshedProcs}
	updatedModel, _ := m.Update(msg)
	newModel := updatedModel.(Model)

	// In the sorted-by-name list:
	// Index 0 should be "a-app" (ID: 2)
	// Index 1 should be "b-app" (ID: 1)
	// Since we selected "a-app" (ID: 2) before, after refresh/sort, the selected index should be 0.
	if newModel.selected != 0 {
		t.Errorf("Expected selected index to be 0 (pointing to a-app, ID: 2), but got %d", newModel.selected)
	}
	if newModel.procs[newModel.selected].ID != 2 {
		t.Errorf("Expected selected process ID to be 2, but got %d", newModel.procs[newModel.selected].ID)
	}

	// Now try another case where we select the second process: "b-app" (ID: 1)
	m.selected = 1 // points to "b-app" (ID 1)
	updatedModel2, _ := m.Update(msg)
	newModel2 := updatedModel2.(Model)

	// After refresh and sorting by Name, "b-app" is at Index 1.
	// So selected index should be 1.
	if newModel2.selected != 1 {
		t.Errorf("Expected selected index to be 1 (pointing to b-app, ID: 1), but got %d", newModel2.selected)
	}
	if newModel2.procs[newModel2.selected].ID != 1 {
		t.Errorf("Expected selected process ID to be 1, but got %d", newModel2.procs[newModel2.selected].ID)
	}
}

