package logbrowser

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/pm2/logfile"
	"github.com/bizshuk/pm2/process"
)

func TestApplicationToFilesToViewerAndBack(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "daemon.log")
	if err := os.WriteFile(logPath, []byte("line one\nline two\nline three\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	m := modelWithApplications([]process.ProcessInfo{testProcess(logPath)})

	m, cmd := updateKey(t, m, "enter")
	if m.screen != screenFiles {
		t.Fatalf("screen after application Enter = %v, want files", m.screen)
	}
	if cmd == nil {
		t.Fatal("application Enter command = nil, want file discovery")
	}
	m = updateMessage(t, m, cmd())
	if len(m.files) != 1 || m.files[0].Path != logPath {
		t.Fatalf("files = %#v, want %q", m.files, logPath)
	}

	m, cmd = updateKey(t, m, "enter")
	if m.screen != screenViewer {
		t.Fatalf("screen after file Enter = %v, want viewer", m.screen)
	}
	if cmd == nil {
		t.Fatal("file Enter command = nil, want file read")
	}
	m = updateMessage(t, m, cmd())
	if len(m.lines) != 3 {
		t.Fatalf("lines = %#v, want 3", m.lines)
	}
	if m.lineCursor != 2 {
		t.Fatalf("lineCursor = %d, want latest line 2", m.lineCursor)
	}

	m, _ = updateKey(t, m, "up")
	if m.lineCursor != 1 {
		t.Fatalf("lineCursor after up = %d, want 1", m.lineCursor)
	}
	m, _ = updateKey(t, m, "down")
	if m.lineCursor != 2 {
		t.Fatalf("lineCursor after down = %d, want 2", m.lineCursor)
	}
	m, _ = updateKey(t, m, "esc")
	if m.screen != screenFiles {
		t.Fatalf("screen after viewer Esc = %v, want files", m.screen)
	}
	m, _ = updateKey(t, m, "esc")
	if m.screen != screenApplications {
		t.Fatalf("screen after files Esc = %v, want applications", m.screen)
	}
}

func TestDeleteRequiresExplicitYes(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "daemon.2026-07-29.log")
	if err := os.WriteFile(path, []byte("archive\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	m := modelWithFiles(testProcess(path), []logfile.FileInfo{{
		Path: path,
		Name: filepath.Base(path),
	}})

	m, cmd := updateKey(t, m, "d")
	if cmd != nil {
		t.Fatalf("d command = %v, want confirmation only", cmd)
	}
	if m.screen != screenConfirmDelete {
		t.Fatalf("screen after d = %v, want confirm", m.screen)
	}
	m, _ = updateKey(t, m, "n")
	if m.screen != screenFiles {
		t.Fatalf("screen after n = %v, want files", m.screen)
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
	if m.screen != screenFiles {
		t.Fatalf("screen after delete = %v, want files", m.screen)
	}
	if refreshCmd == nil {
		t.Fatal("delete result command = nil, want refreshed file list")
	}
	m = updateMessage(t, m, refreshCmd())
	if len(m.files) != 0 {
		t.Fatalf("files after delete = %#v, want empty", m.files)
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

	if m.appSelected != 1 {
		t.Fatalf("appSelected = %d, want matching worker at index 1", m.appSelected)
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
		screen:       screenApplications,
		applications: applications,
		width:        100,
		height:       30,
	}
}

func modelWithFiles(application process.ProcessInfo, files []logfile.FileInfo) Model {
	return Model{
		screen:       screenFiles,
		applications: []process.ProcessInfo{application},
		files:        files,
		width:        100,
		height:       30,
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
