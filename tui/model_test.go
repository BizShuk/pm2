package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/bizshuk/pm2/process"
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

func TestDetailTuiStability(t *testing.T) {
	// Setup a model with 2 processes in detail mode
	m := New("mock_socket", true)
	m.procs = []process.ProcessInfo{
		{ID: 1, Name: "proc-1", LogFile: "/path/to/proc1.log"},
		{ID: 2, Name: "proc-2", LogFile: "/path/to/proc2.log"},
	}
	m.selected = 0
	m.logs = []string{"old log 1", "old log 2"}

	// 1. Verify that logsMsg for a different path is ignored
	ignoredMsg := logsMsg{
		path:  "/path/to/proc2.log",
		lines: []string{"proc-2 log line"},
	}
	resModel, _ := m.Update(ignoredMsg)
	newModel := resModel.(Model)
	if len(newModel.logs) != 2 || newModel.logs[0] != "old log 1" {
		t.Errorf("Expected logs to remain unchanged, but got: %v", newModel.logs)
	}

	// 2. Verify that logsMsg for the current path is accepted
	correctMsg := logsMsg{
		path:  "/path/to/proc1.log",
		lines: []string{"proc-1 log line"},
	}
	resModel2, _ := m.Update(correctMsg)
	newModel2 := resModel2.(Model)
	if len(newModel2.logs) != 1 || newModel2.logs[0] != "proc-1 log line" {
		t.Errorf("Expected logs to be updated, but got: %v", newModel2.logs)
	}

	// 3. Verify that moving the cursor clears the logs immediately
	// Simulate pressing Down key 'j'
	resModel3, _ := newModel2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	newModel3 := resModel3.(Model)
	if newModel3.selected != 1 {
		t.Errorf("Expected selection to change to 1, got %d", newModel3.selected)
	}
	if newModel3.logs != nil {
		t.Errorf("Expected logs to be cleared on cursor move, but got: %v", newModel3.logs)
	}

	// 4. Verify buildLogs shows loading... when logs is nil
	logsOutputNil := newModel3.buildLogs("proc-2", 40, 5)
	if !strings.Contains(logsOutputNil, "loading...") {
		t.Errorf("Expected log output to contain 'loading...', but got: %q", logsOutputNil)
	}

	// 5. Verify buildLogs shows (no log entries) when logs is empty slice
	emptyMsg := logsMsg{
		path:  "/path/to/proc2.log",
		lines: nil, // will trigger []string{} set in Update
	}
	resModel4, _ := newModel3.Update(emptyMsg)
	newModel4 := resModel4.(Model)
	if newModel4.logs == nil || len(newModel4.logs) != 0 {
		t.Errorf("Expected logs to be empty slice, got: %v", newModel4.logs)
	}
	logsOutputEmpty := newModel4.buildLogs("proc-2", 40, 5)
	if !strings.Contains(logsOutputEmpty, "(no log entries)") {
		t.Errorf("Expected log output to contain '(no log entries)', but got: %q", logsOutputEmpty)
	}
}

func TestCroppingUTF8AndRunewidth(t *testing.T) {
	// 1. Test crop (left crop, keep suffix)
	// ASCII string
	res1 := crop("abcdefghij", 6) // maxLen 6 -> "…fghij" (width 1 + 5 = 6)
	if res1 != "…fghij" {
		t.Errorf("Expected '…fghij', got %q", res1)
	}

	// Chinese characters (each has width 2)
	// "一二三四五" (visual width 10)
	// maxLen 6 -> targetWidth = 5.
	// suffix "四五" has width 4. Plus "…" (width 1) = 5 <= 6.
	res2 := crop("一二三四五", 6)
	if res2 != "…四五" {
		t.Errorf("Expected '…四五', got %q", res2)
	}

	// 2. Test cropRight (right crop, keep prefix)
	// ASCII string
	res3 := cropRight("abcdefghij", 6) // maxLen 6 -> "abcde…" (width 5 + 1 = 6)
	if res3 != "abcde…" {
		t.Errorf("Expected 'abcde…', got %q", res3)
	}

	// Chinese characters
	// "一二三四五"
	// maxLen 6 -> targetWidth = 5.
	// prefix "一二" has width 4. Plus "…" (width 1) = 5 <= 6.
	res4 := cropRight("一二三四五", 6)
	if res4 != "一二…" {
		t.Errorf("Expected '一二…', got %q", res4)
	}
}


