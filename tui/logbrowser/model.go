// Package logbrowser owns the interactive log Tree Explorer, Viewer, and
// delete-confirm state machine used by pm2 logs monitor.
//
// It browses the shared config root (~/.config/<app>/logs) rather than the
// daemon's process list: logs outlive the task that wrote them, so a deleted,
// renamed, or never-registered task still has files worth reading. The daemon
// is therefore not consulted at all — the browser works with it down.
package logbrowser

import (
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/pm2/logfile"
	"github.com/bizshuk/pm2/tui/views"
)

type screen uint8

const (
	screenTree screen = iota
	screenViewer
	screenConfirmDelete
)

type appsMsg struct {
	apps []logfile.AppLogs
	err  error
}

type fileMsg struct {
	path  string
	lines []string
	err   error
}

type deletedMsg struct {
	path string
	err  error
}

// Model is the Bubble Tea controller for browsing and deleting log files.
type Model struct {
	root          string
	initialTarget string
	screen        screen
	apps          []logfile.AppLogs
	expanded      map[string]bool
	treeCursor    int
	viewerPath    string
	lines         []string
	lineCursor    int
	width         int
	height        int
	loading       bool
	err           error
	notice        string
}

// New returns a log browser rooted at the application Tree Explorer for every
// application under root. initialTarget names an application directory and
// only affects which row starts selected and expanded.
func New(root, initialTarget string) Model {
	return Model{
		root:          root,
		initialTarget: initialTarget,
		screen:        screenTree,
		expanded:      make(map[string]bool),
		width:         100,
		height:        30,
		loading:       true,
	}
}

// Init scans the config root for application log directories.
func (m Model) Init() tea.Cmd {
	return loadApps(m.root)
}

// Update folds one Bubble Tea message into the log browser.
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		return m.handleKey(msg)
	case appsMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.apps = msg.apps
			m.ensureExpanded()
			m.applyInitialTarget()
			m.treeCursor = clampIndex(m.treeCursor, len(m.visibleTreeRows()))
		}
	case fileMsg:
		if m.viewerPath != msg.path {
			break
		}
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.lines = msg.lines
			m.lineCursor = max(0, len(m.lines)-1)
		}
	case deletedMsg:
		m.screen = screenTree
		if msg.err != nil {
			m.loading = false
			m.err = msg.err
			m.notice = ""
			break
		}
		m.err = nil
		m.notice = "deleted " + filepath.Base(msg.path)
		if m.viewerPath == msg.path {
			m.viewerPath = ""
			m.lines = nil
			m.lineCursor = 0
		}
		// The removed row disappears on rescan, so the cursor lands on
		// whatever now occupies its position rather than jumping away.
		m.loading = true
		return m, loadApps(m.root)
	}
	return m, nil
}

// View delegates all presentation to the pure tui/views renderer.
func (m Model) View() string {
	context := views.LogBrowserContext{
		Width:       m.width,
		Height:      m.height,
		Breadcrumb:  m.breadcrumb(),
		Items:       m.treeItems(),
		Selected:    m.treeCursor,
		Lines:       m.lines,
		LineCursor:  m.lineCursor,
		ViewerPath:  m.viewerPath,
		Viewer:      m.screen == screenViewer,
		CanDelete:   m.canDelete(),
		Loading:     m.loading,
		Empty:       "(no log files under " + m.root + ")",
		Notice:      m.notice,
		Err:         m.err,
		ConfirmPath: m.confirmPath(),
	}
	return views.RenderLogBrowser(context)
}

// applyInitialTarget consumes the command-line target once: a rescan after a
// delete must not drag the cursor back to where the session started.
func (m *Model) applyInitialTarget() {
	if m.initialTarget == "" {
		return
	}
	target := m.initialTarget
	m.initialTarget = ""
	for index, app := range m.apps {
		if !strings.EqualFold(app.App, target) {
			continue
		}
		m.expanded[app.App] = true
		m.treeCursor = m.treeIndexForApp(index)
		return
	}
}

func (m Model) breadcrumb() []string {
	parts := []string{m.root}
	row, ok := m.selectedTreeRow()
	if ok {
		parts = append(parts, m.apps[row.appIndex].App)
	}
	if m.viewerPath != "" {
		parts = append(parts, filepath.Base(m.viewerPath))
	}
	return parts
}

func (m Model) confirmPath() string {
	if m.screen != screenConfirmDelete {
		return ""
	}
	return m.selectedFilePath()
}

func (m Model) canDelete() bool {
	if m.screen != screenTree {
		return false
	}
	row, ok := m.selectedTreeRow()
	return ok && row.kind == treeFile
}

func (m *Model) ensureExpanded() {
	if m.expanded == nil {
		m.expanded = make(map[string]bool)
	}
}

func clampIndex(index, length int) int {
	if length == 0 {
		return 0
	}
	return max(0, min(index, length-1))
}

var _ tea.Model = Model{}
