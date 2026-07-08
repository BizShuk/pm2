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

// TestRecomputeNamespaces checks that the chip strip is built from the
// unfiltered process list: "All" prepended at index 0, every distinct
// namespace present, sorted, and an empty Namespace field normalised
// to "default" (matches process.AppConfig.Normalize).
func TestRecomputeNamespaces(t *testing.T) {
	m := New("sock", false)
	m.allProcs = []process.ProcessInfo{
		{AppConfig: process.AppConfig{Name: "a", Namespace: "prod"}},
		{AppConfig: process.AppConfig{Name: "b", Namespace: "dev"}},
		{AppConfig: process.AppConfig{Name: "c", Namespace: "prod"}},
		{AppConfig: process.AppConfig{Name: "d", Namespace: "staging"}},
		{AppConfig: process.AppConfig{Name: "e"}}, // empty namespace → "default"
	}
	m.recomputeNamespaces()

	want := []string{"All", "default", "dev", "prod", "staging"}
	if len(m.namespaces) != len(want) {
		t.Fatalf("namespaces = %v, want %v", m.namespaces, want)
	}
	for i, n := range want {
		if m.namespaces[i] != n {
			t.Errorf("namespaces[%d] = %q, want %q (full: %v)", i, m.namespaces[i], n, m.namespaces)
		}
	}
	if m.nsCursor != 0 {
		t.Errorf("nsCursor = %d, want 0 (All)", m.nsCursor)
	}
}

// TestApplyNamespaceFilter confirms that switching the chip restricts
// the visible list to the selected namespace, and "All" restores the
// full set.
func TestApplyNamespaceFilter(t *testing.T) {
	m := New("sock", false)
	m.SortBy = SortByName
	m.allProcs = []process.ProcessInfo{
		{AppConfig: process.AppConfig{Name: "a", Namespace: "prod"}, ID: 1},
		{AppConfig: process.AppConfig{Name: "b", Namespace: "dev"}, ID: 2},
		{AppConfig: process.AppConfig{Name: "c", Namespace: "prod"}, ID: 3},
		{AppConfig: process.AppConfig{Name: "d", Namespace: "staging"}, ID: 4},
	}
	m.recomputeNamespaces()
	// ["All", "dev", "prod", "staging"]

	// 1. "All" (cursor 0) shows everything, sorted by name.
	m.applyNamespaceFilter()
	if len(m.procs) != 4 {
		t.Errorf("All: got %d procs, want 4", len(m.procs))
	}

	// 2. "prod" (cursor 2) filters to a-app and c-app only.
	m.nsCursor = 2
	m.applyNamespaceFilter()
	if len(m.procs) != 2 {
		t.Errorf("prod: got %d procs, want 2", len(m.procs))
	}
	for _, p := range m.procs {
		if p.Namespace != "prod" {
			t.Errorf("prod filter leaked %q (ns=%q)", p.Name, p.Namespace)
		}
	}

	// 3. "dev" (cursor 1) filters to one row.
	m.nsCursor = 1
	m.applyNamespaceFilter()
	if len(m.procs) != 1 || m.procs[0].Name != "b" {
		t.Errorf("dev: got %v, want [b]", m.procs)
	}

	// 4. Cursor that no longer maps to a valid chip falls back to 0.
	m.nsCursor = 99
	m.applyNamespaceFilter()
	if m.nsCursor != 0 || len(m.procs) != 4 {
		t.Errorf("fallback: nsCursor=%d procs=%d, want 0 / 4", m.nsCursor, len(m.procs))
	}
}

// TestCycleNamespaceWraps verifies that moving the cursor right past
// the last chip loops back to "All", and left from "All" loops to the
// last chip.
func TestCycleNamespaceWraps(t *testing.T) {
	m := New("sock", false)
	m.SortBy = SortByName
	m.allProcs = []process.ProcessInfo{
		{AppConfig: process.AppConfig{Name: "a", Namespace: "prod"}, ID: 1},
		{AppConfig: process.AppConfig{Name: "b", Namespace: "dev"}, ID: 2},
	}
	m.recomputeNamespaces() // ["All", "dev", "prod"]
	if len(m.namespaces) != 3 {
		t.Fatalf("setup: namespaces = %v, want 3 entries", m.namespaces)
	}

	m.cycleNamespace(+1)
	if m.nsCursor != 1 || m.namespaces[m.nsCursor] != "dev" {
		t.Errorf("right 1: cursor=%d (%q), want 1 (dev)", m.nsCursor, m.namespaces[m.nsCursor])
	}
	m.cycleNamespace(+1)
	if m.nsCursor != 2 || m.namespaces[m.nsCursor] != "prod" {
		t.Errorf("right 2: cursor=%d (%q), want 2 (prod)", m.nsCursor, m.namespaces[m.nsCursor])
	}
	m.cycleNamespace(+1)
	if m.nsCursor != 0 {
		t.Errorf("right wrap: cursor=%d, want 0 (All)", m.nsCursor)
	}
	m.cycleNamespace(-1)
	if m.nsCursor != len(m.namespaces)-1 {
		t.Errorf("left wrap: cursor=%d, want %d (last)", m.nsCursor, len(m.namespaces)-1)
	}
}

// TestLeftRightArrowSwitchesNamespace sends bubble tea key events for
// the actual ←/→ arrow keys and confirms nsCursor + m.procs both
// update. Uses the same KeyMsg shape the runtime produces.
func TestLeftRightArrowSwitchesNamespace(t *testing.T) {
	m := New("sock", false)
	m.SortBy = SortByName
	m.allProcs = []process.ProcessInfo{
		{AppConfig: process.AppConfig{Name: "a", Namespace: "prod"}, ID: 1},
		{AppConfig: process.AppConfig{Name: "b", Namespace: "dev"}, ID: 2},
	}
	m.recomputeNamespaces()
	m.applyNamespaceFilter() // nsCursor=0, "All", 2 procs

	if len(m.procs) != 2 {
		t.Fatalf("setup: All-view should show 2 procs, got %d", len(m.procs))
	}

	// Press Right → "dev"
	res, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = res.(Model)
	if m.namespaces[m.nsCursor] != "dev" {
		t.Errorf("after →: cursor=%d (%q), want dev", m.nsCursor, m.namespaces[m.nsCursor])
	}
	if len(m.procs) != 1 || m.procs[0].Name != "b" {
		t.Errorf("after →: procs=%v, want [b]", m.procs)
	}

	// Press Right again → "prod"
	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = res.(Model)
	if m.namespaces[m.nsCursor] != "prod" {
		t.Errorf("after →: cursor=%q, want prod", m.namespaces[m.nsCursor])
	}
	if len(m.procs) != 1 || m.procs[0].Name != "a" {
		t.Errorf("after →: procs=%v, want [a]", m.procs)
	}

	// Press Left → "dev"
	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = res.(Model)
	if m.namespaces[m.nsCursor] != "dev" {
		t.Errorf("after ←: cursor=%q, want dev", m.namespaces[m.nsCursor])
	}
}

// TestRefreshPreservesNamespaceCursor checks that the cursor follows
// the namespace by name across a refresh, and falls back to 0 when
// the active namespace disappears (last process in it exited).
func TestRefreshPreservesNamespaceCursor(t *testing.T) {
	m := New("sock", false)
	m.SortBy = SortByName
	m.allProcs = []process.ProcessInfo{
		{AppConfig: process.AppConfig{Name: "a", Namespace: "prod"}, ID: 1},
		{AppConfig: process.AppConfig{Name: "b", Namespace: "dev"}, ID: 2},
		{AppConfig: process.AppConfig{Name: "c", Namespace: "prod"}, ID: 3},
	}
	m.recomputeNamespaces()
	m.applyNamespaceFilter()
	m.nsCursor = 2 // "prod"

	// Refresh with the same namespaces — cursor must stay on "prod".
	res, _ := m.Update(refreshMsg{procs: []process.ProcessInfo{
		{AppConfig: process.AppConfig{Name: "a", Namespace: "prod"}, ID: 1},
		{AppConfig: process.AppConfig{Name: "b", Namespace: "dev"}, ID: 2},
		{AppConfig: process.AppConfig{Name: "x", Namespace: "staging"}, ID: 9},
	}})
	m = res.(Model)
	if m.namespaces[m.nsCursor] != "prod" {
		t.Errorf("stable ns: cursor=%q, want prod", m.namespaces[m.nsCursor])
	}
	if len(m.procs) != 1 || m.procs[0].Name != "a" {
		t.Errorf("stable ns: procs=%v, want [a]", m.procs)
	}

	// Refresh with no prod left — cursor must fall back to 0 ("All").
	res, _ = m.Update(refreshMsg{procs: []process.ProcessInfo{
		{AppConfig: process.AppConfig{Name: "b", Namespace: "dev"}, ID: 2},
		{AppConfig: process.AppConfig{Name: "x", Namespace: "staging"}, ID: 9},
	}})
	m = res.(Model)
	if m.nsCursor != 0 {
		t.Errorf("lost ns: cursor=%d, want 0 (All)", m.nsCursor)
	}
	if m.namespaces[m.nsCursor] != "All" {
		t.Errorf("lost ns: chip=%q, want All", m.namespaces[m.nsCursor])
	}
	if len(m.procs) != 2 {
		t.Errorf("lost ns: procs=%d, want 2 (All-view)", len(m.procs))
	}
}

// TestEnterTogglesLogFocus verifies that in two-pane (Detail) mode the
// Enter key flips log-focus on and off, and produces no command (it's
// pure local UI state).
func TestEnterTogglesLogFocus(t *testing.T) {
	m := New("sock", true)
	m.procs = []process.ProcessInfo{{AppConfig: process.AppConfig{Name: "p"}, ID: 1}}

	res, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = res.(Model)
	if !m.logFocus {
		t.Errorf("after first Enter: logFocus = false, want true")
	}
	if cmd != nil {
		t.Errorf("first Enter produced a command, want nil; got %T", cmd)
	}

	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = res.(Model)
	if m.logFocus {
		t.Errorf("after second Enter: logFocus = true, want false (toggle)")
	}
}

// TestEscExitsLogFocus verifies Esc only acts when log-focus is on and
// never changes the Detail flag.
func TestEscExitsLogFocus(t *testing.T) {
	m := New("sock", true)
	m.procs = []process.ProcessInfo{{AppConfig: process.AppConfig{Name: "p"}, ID: 1}}
	m.logFocus = true

	res, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = res.(Model)
	if m.logFocus {
		t.Errorf("Esc in log-focus: logFocus = true, want false")
	}
	if !m.Detail {
		t.Errorf("Esc clobbered Detail: Detail = false, want true")
	}

	// Esc from non-log-focus state is a no-op.
	m.logFocus = false
	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = res.(Model)
	if m.logFocus {
		t.Errorf("Esc on logFocus=false flipped it to true")
	}
}

// TestEnterIsNoopInWideTable confirms log-focus is a sub-mode of the
// two-pane layout and is inert in wide-table mode.
func TestEnterIsNoopInWideTable(t *testing.T) {
	m := New("sock", false) // wide-table
	m.procs = []process.ProcessInfo{{AppConfig: process.AppConfig{Name: "p"}, ID: 1}}

	res, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = res.(Model)
	if m.logFocus {
		t.Errorf("Enter in wide-table set logFocus = true, want false")
	}
}

// TestRenderRightPaneLogFocusHidesDetail confirms the renderer drops the
// detail block and uses the full right-pane height when LogFocus is on.
func TestRenderRightPaneLogFocusHidesDetail(t *testing.T) {
	ctx := views.ViewContext{
		Width:    60,
		Height:   20,
		Selected: 0,
		Procs: []process.ProcessInfo{{
			AppConfig: process.AppConfig{Name: "p", Script: "/tmp/script.sh"},
			ID:        1,
		}},
		Logs:     []string{"line 1"},
		LogFocus: true,
	}
	out := views.RenderRightPane(ctx, 50, 18)
	if strings.Contains(out, "detail —") {
		t.Errorf("log-focus output should not contain 'detail —', got: %q", out)
	}
	if strings.Contains(out, "DETAIL —") {
		t.Errorf("log-focus output should not contain 'DETAIL —', got: %q", out)
	}
	if strings.Contains(out, "script") {
		t.Errorf("log-focus output should not contain 'script' (detail row), got: %q", out)
	}
	if !strings.Contains(out, "LOGS —") {
		t.Errorf("log-focus output should contain 'LOGS —', got: %q", out)
	}
	if !strings.Contains(out, "line 1") {
		t.Errorf("log-focus output should contain the log line, got: %q", out)
	}

	// Control: with LogFocus off, the detail block is present.
	ctx.LogFocus = false
	out2 := views.RenderRightPane(ctx, 50, 18)
	if !strings.Contains(out2, "DETAIL —") {
		t.Errorf("control: non-log-focus output should contain 'DETAIL —', got: %q", out2)
	}
}

// TestFooterIncludesLogFocusHint confirms the footer advertises the
// new Enter/Esc binding so the user can discover it.
func TestFooterIncludesLogFocusHint(t *testing.T) {
	out := views.RenderFooter(120, "name")
	if !strings.Contains(out, "⏎/esc") {
		t.Errorf("footer missing '⏎/esc' hint, got: %q", out)
	}
	if !strings.Contains(out, "logs only") {
		t.Errorf("footer missing 'logs only' hint, got: %q", out)
	}
}

// TestArrowKeysWorkOnEmptyFilteredList confirms the user can escape
// an empty filter (e.g. landed on a namespace with no procs) using
// the arrow keys, even though r/p/d remain no-ops on an empty list.
func TestArrowKeysWorkOnEmptyFilteredList(t *testing.T) {
	m := New("sock", false)
	m.SortBy = SortByName
	m.procs = nil // force an empty filtered view

	// r/p/d on empty filtered list must be no-ops (the existing
	// empty-procs guard stays intact for action keys).
	for _, key := range []string{"r", "p", "d"} {
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if cmd != nil {
			t.Errorf("%s on empty filtered list should not produce a command, got %T", key, cmd)
		}
	}

	// ←/→ must be safe to call even with no namespaces yet — the
	// handler should not crash and should not produce a command.
	for _, k := range []tea.KeyType{tea.KeyLeft, tea.KeyRight} {
		_, cmd := m.Update(tea.KeyMsg{Type: k})
		if cmd != nil {
			t.Errorf("arrow on empty namespaces should not produce a command, got %T", cmd)
		}
	}
}
