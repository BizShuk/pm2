package logbrowser

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/pm2/logfile"
	"github.com/bizshuk/pm2/process"
)

func TestTreeRightOpensAndLeftReturns(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "daemon.log")
	if err := os.WriteFile(logPath, []byte("line one\nline two\nline three\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	m := modelWithApplications([]process.ProcessInfo{testProcess(logPath)})

	m, cmd := updateKey(t, m, "right")
	if m.screen != screenTree {
		t.Fatalf("screen after application Right = %v, want tree", m.screen)
	}
	if cmd == nil {
		t.Fatal("application Right command = nil, want file discovery")
	}
	m = updateMessage(t, m, cmd())
	if rows := m.visibleTreeRows(); len(rows) != 2 {
		t.Fatalf("visible rows = %#v, want application plus file", rows)
	}

	m, _ = updateKey(t, m, "down")
	m, cmd = updateKey(t, m, "right")
	if m.screen != screenViewer {
		t.Fatalf("screen after file Right = %v, want viewer", m.screen)
	}
	if cmd == nil {
		t.Fatal("file Right command = nil, want file read")
	}
	m = updateMessage(t, m, cmd())
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
		t.Fatalf("visible rows after file Left = %#v, want collapsed application", rows)
	}
	if m.treeCursor != 0 {
		t.Fatalf("treeCursor after collapse = %d, want parent application 0", m.treeCursor)
	}
}

func TestTreeEnterFocusesViewerAndLoadsSelectedFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "daemon.log")
	if err := os.WriteFile(path, []byte("first\nsecond\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	m := modelWithFiles(testProcess(path), []logfile.FileInfo{{Path: path}})

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

	path := filepath.Join(t.TempDir(), "daemon.2026-07-29.log")
	if err := os.WriteFile(path, []byte("archive\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	m := modelWithFiles(testProcess(path), []logfile.FileInfo{{
		Path: path,
		Name: filepath.Base(path),
	}})

	m.treeCursor = 0
	m, cmd := updateKey(t, m, "d")
	if cmd != nil {
		t.Fatalf("d on application command = %v, want nil", cmd)
	}
	if m.screen != screenTree {
		t.Fatalf("screen after d on application = %v, want tree", m.screen)
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
		t.Fatal("delete result command = nil, want refreshed file list")
	}
	m = updateMessage(t, m, refreshCmd())
	if files := m.filesByApplication[0]; len(files) != 0 {
		t.Fatalf("files after delete = %#v, want empty", files)
	}
}

func TestViewerDoesNotDeleteFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "daemon.log")
	m := modelWithFiles(testProcess(path), []logfile.FileInfo{{Path: path}})
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

	path := filepath.Join(t.TempDir(), "daemon.log")
	if err := os.WriteFile(path, []byte("line\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	m := modelWithFiles(testProcess(path), []logfile.FileInfo{{Path: path}})

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

func TestInitialTargetSelectsMatchingApplication(t *testing.T) {
	t.Parallel()

	m := New("unused.sock", "worker")
	msg := applicationsMsg{applications: []process.ProcessInfo{
		{AppConfig: process.AppConfig{Name: "api"}, ID: 1},
		{AppConfig: process.AppConfig{Name: "worker"}, ID: 2},
	}}
	m = updateMessage(t, m, msg)

	if m.treeCursor != 1 {
		t.Fatalf("treeCursor = %d, want matching worker at index 1", m.treeCursor)
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

func testProcess(logPath string) process.ProcessInfo {
	return process.ProcessInfo{
		AppConfig: process.AppConfig{
			Namespace: "default",
			Name:      "worker",
			LogFile:   logPath,
			ErrorFile: filepath.Join(filepath.Dir(logPath), "daemon.err"),
		},
		ID:     7,
		Status: process.StatusOnline,
	}
}

func modelWithApplications(applications []process.ProcessInfo) Model {
	return Model{
		screen:             screenTree,
		applications:       applications,
		expanded:           make(map[int]bool),
		filesByApplication: make(map[int][]logfile.FileInfo),
		width:              100,
		height:             30,
	}
}

func modelWithFiles(application process.ProcessInfo, files []logfile.FileInfo) Model {
	return Model{
		screen:       screenTree,
		applications: []process.ProcessInfo{application},
		expanded: map[int]bool{
			0: true,
		},
		filesByApplication: map[int][]logfile.FileInfo{
			0: files,
		},
		treeCursor: 1,
		width:      100,
		height:     30,
	}
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
