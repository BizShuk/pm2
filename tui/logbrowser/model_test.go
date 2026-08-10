package logbrowser

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/pm2/logfile"
)

func TestTreeRightOpensAndLeftReturns(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	logPath := writeAppLog(t, root, "worker", "daemon.log", "line one\nline two\nline three\n")
	m := loadedModel(t, root)

	m, _ = updateKey(t, m, "right")
	if m.screen != screenTree {
		t.Fatalf("screen after app Right = %v, want tree", m.screen)
	}
	if rows := m.visibleTreeRows(); len(rows) != 2 {
		t.Fatalf("visible rows = %#v, want app plus file", rows)
	}

	m, _ = updateKey(t, m, "down")
	m, cmd := updateKey(t, m, "right")
	if m.screen != screenViewer {
		t.Fatalf("screen after file Right = %v, want viewer", m.screen)
	}
	if cmd == nil {
		t.Fatal("file Right command = nil, want file read")
	}
	m = updateMessage(t, m, cmd())
	if m.viewerPath != logPath {
		t.Fatalf("viewerPath = %q, want %q", m.viewerPath, logPath)
	}
	if len(m.lines) != 3 {
		t.Fatalf("lines = %#v, want 3", m.lines)
	}
	if m.lineCursor != 2 {
		t.Fatalf("lineCursor = %d, want latest line 2", m.lineCursor)
	}

	m, _ = updateKey(t, m, "left")
	if m.screen != screenTree {
		t.Fatalf("screen after viewer Left = %v, want tree", m.screen)
	}
	m, _ = updateKey(t, m, "left")
	if rows := m.visibleTreeRows(); len(rows) != 1 {
		t.Fatalf("visible rows after file Left = %#v, want collapsed app", rows)
	}
	if m.treeCursor != 0 {
		t.Fatalf("treeCursor after collapse = %d, want parent app 0", m.treeCursor)
	}
}

// The listing is the filesystem's, not the daemon's: an application whose task
// is long gone still owns its log directory and must appear.
func TestScanListsEveryApplicationUnderTheConfigRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeAppLog(t, root, "vidnote", "daemon.log", "a\n")
	writeAppLog(t, root, "agentmemory", "daemon.log", "b\n")
	writeAppLog(t, root, "agentmemory", "daemon.2026-07-29.log", "c\n")
	if err := os.MkdirAll(filepath.Join(root, "no-logs", "data"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	m := loadedModel(t, root)
	if len(m.apps) != 2 {
		t.Fatalf("apps = %#v, want agentmemory and vidnote", m.apps)
	}
	if m.apps[0].App != "agentmemory" || m.apps[1].App != "vidnote" {
		t.Fatalf("apps = %q, %q, want them sorted by name", m.apps[0].App, m.apps[1].App)
	}
	if got := len(m.apps[0].Files); got != 2 {
		t.Fatalf("agentmemory files = %d, want current plus archive", got)
	}
}

func TestTreeEnterFocusesViewerAndLoadsSelectedFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := writeAppLog(t, root, "worker", "daemon.log", "first\nsecond\n")
	m := expandedModel(t, root)

	m, loadCmd := updateKey(t, m, "enter")
	if m.screen != screenViewer {
		t.Fatalf("screen after file Enter = %v, want viewer focus", m.screen)
	}
	if loadCmd == nil {
		t.Fatal("file Enter command = nil, want file read")
	}

	m = updateMessage(t, m, loadCmd())
	if got, want := m.viewerPath, path; got != want {
		t.Fatalf("viewerPath = %q, want %q", got, want)
	}
	if len(m.lines) != 2 || m.lineCursor != 1 {
		t.Fatalf("loaded lines = %#v cursor %d, want two lines at cursor 1", m.lines, m.lineCursor)
	}
}

func TestTreeDeleteIsAvailableOnlyOnFileRow(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := writeAppLog(t, root, "worker", "daemon.2026-07-29.log", "archive\n")
	m := expandedModel(t, root)

	m.treeCursor = 0
	m, cmd := updateKey(t, m, "d")
	if cmd != nil {
		t.Fatalf("d on app command = %v, want nil", cmd)
	}
	if m.screen != screenTree {
		t.Fatalf("screen after d on app = %v, want tree", m.screen)
	}

	m.treeCursor = 1
	m, cmd = updateKey(t, m, "d")
	if cmd != nil {
		t.Fatalf("d command = %v, want confirmation only", cmd)
	}
	if m.screen != screenConfirmDelete {
		t.Fatalf("screen after d on file = %v, want confirm", m.screen)
	}
	m, _ = updateKey(t, m, "n")
	if m.screen != screenTree {
		t.Fatalf("screen after n = %v, want tree", m.screen)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file removed without confirmation: %v", err)
	}

	m, _ = updateKey(t, m, "d")
	m, cmd = updateKey(t, m, "y")
	if cmd == nil {
		t.Fatal("y command = nil, want deletion")
	}
	m, refreshCmd := updateMessageWithCmd(t, m, cmd())
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file still exists after y: %v", err)
	}
	if m.screen != screenTree {
		t.Fatalf("screen after delete = %v, want tree", m.screen)
	}
	if refreshCmd == nil {
		t.Fatal("delete result command = nil, want rescan")
	}
	m = updateMessage(t, m, refreshCmd())
	if len(m.apps) != 0 {
		t.Fatalf("apps after deleting the only log = %#v, want empty", m.apps)
	}
}

// A rescan keeps the user where they were: expansion is keyed by application
// name, not by a row index that shifts when a file disappears.
func TestRescanKeepsExpansionAcrossDelete(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeAppLog(t, root, "worker", "daemon.log", "current\n")
	writeAppLog(t, root, "worker", "daemon.2026-07-29.log", "archive\n")
	m := expandedModel(t, root)

	m.treeCursor = 2
	m, _ = updateKey(t, m, "d")
	m, cmd := updateKey(t, m, "y")
	m, refreshCmd := updateMessageWithCmd(t, m, cmd())
	m = updateMessage(t, m, refreshCmd())

	if !m.expanded["worker"] {
		t.Fatal("worker collapsed after rescan, want it still expanded")
	}
	if rows := m.visibleTreeRows(); len(rows) != 2 {
		t.Fatalf("visible rows after delete = %#v, want app plus one file", rows)
	}
	if m.notice == "" {
		t.Error("notice = empty, want the deleted file named")
	}
}

func TestViewerDoesNotDeleteFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeAppLog(t, root, "worker", "daemon.log", "line\n")
	m := expandedModel(t, root)
	m.screen = screenViewer

	m, cmd := updateKey(t, m, "d")
	if cmd != nil {
		t.Fatalf("viewer d command = %v, want nil", cmd)
	}
	if m.screen != screenViewer {
		t.Fatalf("viewer d screen = %v, want viewer", m.screen)
	}
}

func TestViewerLeftReturnsTreeFocusAndKeepsPendingPreview(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := writeAppLog(t, root, "worker", "daemon.log", "line\n")
	m := expandedModel(t, root)

	m, loadCmd := updateKey(t, m, "right")
	if m.screen != screenViewer || !m.loading || loadCmd == nil {
		t.Fatalf("Right state = screen %v loading %v cmd %v, want loading viewer", m.screen, m.loading, loadCmd)
	}
	m, _ = updateKey(t, m, "left")
	if m.screen != screenTree {
		t.Fatalf("Left screen = %v, want tree focus", m.screen)
	}
	if !m.loading || m.viewerPath != path {
		t.Fatalf("Left cleared pending preview: loading %v path %q", m.loading, m.viewerPath)
	}

	m = updateMessage(t, m, loadCmd())
	if m.screen != screenTree || len(m.lines) != 1 {
		t.Fatalf("pending preview result = screen %v lines %#v, want Tree focus with one line", m.screen, m.lines)
	}
}

func TestInitialTargetSelectsAndExpandsMatchingApp(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeAppLog(t, root, "api", "daemon.log", "a\n")
	writeAppLog(t, root, "worker", "daemon.log", "b\n")

	m := New(root, "worker")
	m = updateMessage(t, m, appsMsg{apps: mustListApps(t, root)})

	if m.treeCursor != 1 {
		t.Fatalf("treeCursor = %d, want matching worker at index 1", m.treeCursor)
	}
	if !m.expanded["worker"] {
		t.Fatal("worker not expanded, want the named app opened")
	}
	if m.initialTarget != "" {
		t.Error("initialTarget retained, want it consumed so a rescan does not re-seek")
	}
}

func TestViewerNavigationClampsAtBothEnds(t *testing.T) {
	t.Parallel()

	m := Model{
		screen:     screenViewer,
		lines:      []string{"first", "second"},
		lineCursor: 1,
	}
	for range 3 {
		m, _ = updateKey(t, m, "down")
	}
	if m.lineCursor != 1 {
		t.Fatalf("lineCursor after repeated down = %d, want 1", m.lineCursor)
	}
	for range 3 {
		m, _ = updateKey(t, m, "k")
	}
	if m.lineCursor != 0 {
		t.Fatalf("lineCursor after repeated k = %d, want 0", m.lineCursor)
	}
}

func TestViewerPageNavigationUsesBodyHeight(t *testing.T) {
	t.Parallel()

	m := Model{
		screen:     screenViewer,
		lines:      []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"},
		lineCursor: 9,
		height:     8,
	}

	m, _ = updateKey(t, m, "pgup")
	if m.lineCursor != 5 {
		t.Fatalf("lineCursor after PageUp = %d, want 5", m.lineCursor)
	}
	m, _ = updateKey(t, m, "pgdown")
	if m.lineCursor != 9 {
		t.Fatalf("lineCursor after PageDown = %d, want 9", m.lineCursor)
	}
}

func writeAppLog(t *testing.T, root, app, name, content string) string {
	t.Helper()
	dir := filepath.Join(root, app, logfile.LogsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", dir, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}

func mustListApps(t *testing.T, root string) []logfile.AppLogs {
	t.Helper()
	apps, err := logfile.ListApps(root)
	if err != nil {
		t.Fatalf("ListApps() error = %v", err)
	}
	return apps
}

// loadedModel runs the real Init scan so tests exercise the same path the
// command does.
func loadedModel(t *testing.T, root string) Model {
	t.Helper()
	m := New(root, "")
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() = nil, want the config-root scan")
	}
	return updateMessage(t, m, cmd())
}

// expandedModel loads root and opens its single application, leaving the
// cursor on the first file row.
func expandedModel(t *testing.T, root string) Model {
	t.Helper()
	m := loadedModel(t, root)
	m, _ = updateKey(t, m, "right")
	m.treeCursor = 1
	return m
}

func updateKey(t *testing.T, m Model, key string) (Model, tea.Cmd) {
	t.Helper()
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	switch key {
	case "enter":
		keyMsg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		keyMsg = tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		keyMsg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		keyMsg = tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		keyMsg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		keyMsg = tea.KeyMsg{Type: tea.KeyRight}
	case "pgup":
		keyMsg = tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		keyMsg = tea.KeyMsg{Type: tea.KeyPgDown}
	}
	updated, cmd := m.Update(keyMsg)
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update() model type = %T, want Model", updated)
	}
	return got, cmd
}

func updateMessage(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update() model type = %T, want Model", updated)
	}
	return got
}

func updateMessageWithCmd(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update() model type = %T, want Model", updated)
	}
	return got, cmd
}
