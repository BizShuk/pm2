package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/tui/views"
)

func TestBuildDetailScriptArgsCombined(t *testing.T) {
	_ = New("mock_socket", true)
	p := process.ProcessInfo{
		AppConfig: process.AppConfig{
			Name:   "test-app",
			Script: "/path/to/script.sh",
			Args:   []string{"--foo", "bar", "-v"},
		},
		ID:        1,
		Status:    process.StatusOnline,
		StartedAt: time.Now(),
	}

	detail := views.RenderDetail(p, 100)

	expected := "/path/to/script.sh --foo bar -v"
	if !strings.Contains(detail, expected) {
		t.Errorf("Expected detail to contain %q, but got %q", expected, detail)
	}
}

func TestBuildDetailScriptNoArgs(t *testing.T) {
	_ = New("mock_socket", true)
	p := process.ProcessInfo{
		AppConfig: process.AppConfig{
			Name:   "test-app",
			Script: "/path/to/script.sh",
		},
		ID:        1,
		Status:    process.StatusOnline,
		StartedAt: time.Now(),
	}

	detail := views.RenderDetail(p, 100)

	expected := "/path/to/script.sh"
	if !strings.Contains(detail, expected) {
		t.Errorf("Expected detail to contain %q, but got %q", expected, detail)
	}
}

func TestProcessSorting(t *testing.T) {
	m := New("mock_socket", false)

	p1 := process.ProcessInfo{
		AppConfig: process.AppConfig{Name: "b-app", Namespace: "prod"},
		ID:        1,
		CPU:       5.5,
		Memory:    2048,
		Status:    process.StatusOnline,
	}
	p2 := process.ProcessInfo{
		AppConfig: process.AppConfig{Name: "a-app", Namespace: "dev"},
		ID:        2,
		CPU:       10.2,
		Memory:    1024,
		Status:    process.StatusErrored,
	}
	p3 := process.ProcessInfo{
		AppConfig: process.AppConfig{Name: "c-app", Namespace: "dev"},
		ID:        3,
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
		{AppConfig: process.AppConfig{Name: "a-app"}, ID: 2},
		{AppConfig: process.AppConfig{Name: "b-app"}, ID: 1},
	}
	m.selected = 0 // points to "a-app" (ID 2)

	// Simulate refreshMsg, which receives a list sorted by ID (b-app [ID: 1], a-app [ID: 2])
	refreshedProcs := []process.ProcessInfo{
		{AppConfig: process.AppConfig{Name: "b-app"}, ID: 1},
		{AppConfig: process.AppConfig{Name: "a-app"}, ID: 2},
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
		{AppConfig: process.AppConfig{Name: "proc-1", LogFile: "/path/to/proc1.log"}, ID: 1},
		{AppConfig: process.AppConfig{Name: "proc-2", LogFile: "/path/to/proc2.log"}, ID: 2},
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
	logsOutputNil := views.RenderLogs("proc-2", nil, 40, 5)
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
	logsOutputEmpty := views.RenderLogs("proc-2", []string{}, 40, 5)
	if !strings.Contains(logsOutputEmpty, "(no log entries)") {
		t.Errorf("Expected log output to contain '(no log entries)', but got: %q", logsOutputEmpty)
	}
}

func TestCroppingUTF8AndRunewidth(t *testing.T) {
	// crop / cropRight are pure helpers that live in tui/views/format.go
	// since the view layer is the only consumer. We exercise them
	// here to lock in the runewidth behaviour the renderer depends on.
	// 1. Test crop (left crop, keep suffix)
	// ASCII string
	res1 := views.Crop("abcdefghij", 6) // maxLen 6 -> "…fghij" (width 1 + 5 = 6)
	if res1 != "…fghij" {
		t.Errorf("Expected '…fghij', got %q", res1)
	}

	// Chinese characters (each has width 2)
	// "一二三四五" (visual width 10)
	// maxLen 6 -> targetWidth = 5.
	// suffix "四五" has width 4. Plus "…" (width 1) = 5 <= 6.
	res2 := views.Crop("一二三四五", 6)
	if res2 != "…四五" {
		t.Errorf("Expected '…四五', got %q", res2)
	}

	// 2. Test cropRight (right crop, keep prefix)
	// ASCII string
	res3 := views.CropRight("abcdefghij", 6) // maxLen 6 -> "abcde…" (width 5 + 1 = 6)
	if res3 != "abcde…" {
		t.Errorf("Expected 'abcde…', got %q", res3)
	}

	// Chinese characters
	// "一二三四五"
	// maxLen 6 -> targetWidth = 5.
	// prefix "一二" has width 4. Plus "…" (width 1) = 5 <= 6.
	res4 := views.CropRight("一二三四五", 6)
	if res4 != "一二…" {
		t.Errorf("Expected '一二…', got %q", res4)
	}
}

// TestPauseOrResume verifies the `p` key toggle maps a paused process to
// resume and every other status to pause.
func TestPauseOrResume(t *testing.T) {
	if got := pauseOrResume(process.StatusPaused); got != model.CmdResume {
		t.Errorf("paused → %q, want %q", got, model.CmdResume)
	}
	for _, s := range []process.Status{
		process.StatusOnline,
		process.StatusStopped,
		process.StatusErrored,
		process.StatusLaunching,
	} {
		if got := pauseOrResume(s); got != model.CmdPause {
			t.Errorf("%s → %q, want %q", s, got, model.CmdPause)
		}
	}
}

// TestActionNoticeSurfacesAndClears verifies a failed action's notice is
// shown in the title bar and cleared on the next refresh tick — so a stale
// daemon that rejects `pause`/`resume` is visible rather than silent.
func TestActionNoticeSurfacesAndClears(t *testing.T) {
	m := New("/tmp/x.sock", false)
	m.width = 120

	// Simulate the result of a rejected action (e.g. old daemon).
	updated, _ := m.Update(actionMsg{
		refreshMsg: refreshMsg{procs: nil},
		notice:     "pause failed: unknown command: pause",
	})
	m = updated.(Model)
	if m.notice == "" {
		t.Fatal("notice not recorded on actionMsg")
	}
	if title := views.RenderHeader(views.ViewContext{
		Width:  m.width,
		Notice: m.notice,
	}); !strings.Contains(title, "pause failed") {
		t.Errorf("title bar did not show the action notice, got: %q", title)
	}

	// A subsequent tick clears the transient notice.
	updated, _ = m.Update(tickMsg{})
	m = updated.(Model)
	if m.notice != "" {
		t.Errorf("notice not cleared on tick, got %q", m.notice)
	}
}
