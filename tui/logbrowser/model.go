// Package logbrowser owns the interactive managed-log Tree Explorer, Viewer,
// and delete-confirm state machine used by pm2 logs monitor.
package logbrowser

import (
	"path/filepath"
	"sort"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/pm2/logfile"
	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/tui/views"
)

type screen uint8

const (
	screenTree screen = iota
	screenViewer
	screenConfirmDelete
)

type applicationsMsg struct {
	applications []process.ProcessInfo
	err          error
}

type filesMsg struct {
	applicationIndex int
	files            []logfile.FileInfo
	err              error
}

type fileMsg struct {
	path  string
	lines []string
	err   error
}

type deletedMsg struct {
	applicationIndex int
	path             string
	err              error
}

// Model is the Bubble Tea controller for browsing and deleting managed log
// files.
type Model struct {
	socket             string
	initialTarget      string
	screen             screen
	applications       []process.ProcessInfo
	expanded           map[int]bool
	filesByApplication map[int][]logfile.FileInfo
	treeCursor         int
	viewerPath         string
	lines              []string
	lineCursor         int
	width              int
	height             int
	loading            bool
	err                error
	notice             string
}

// New returns a log browser rooted at the application Tree Explorer.
// initialTarget may be an application ID, name, namespace:name key, or
// namespace and only affects the initially selected application row.
func New(socket, initialTarget string) Model {
	return Model{
		socket:             socket,
		initialTarget:      initialTarget,
		screen:             screenTree,
		expanded:           make(map[int]bool),
		filesByApplication: make(map[int][]logfile.FileInfo),
		width:              100,
		height:             30,
	}
}

// Init loads the current daemon application snapshot.
func (m Model) Init() tea.Cmd {
	return loadApplications(m.socket)
}

// Update folds one Bubble Tea message into the log browser.
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		return m.handleKey(msg)
	case applicationsMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.applications = msg.applications
			sortApplications(m.applications)
			m.treeCursor = matchingApplication(m.applications, m.initialTarget)
			m.initialTarget = ""
			m.expanded = make(map[int]bool)
			m.filesByApplication = make(map[int][]logfile.FileInfo)
		}
	case filesMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil &&
			msg.applicationIndex >= 0 &&
			msg.applicationIndex < len(m.applications) {
			m.ensureTreeMaps()
			m.filesByApplication[msg.applicationIndex] = msg.files
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
		m.loading = false
		m.screen = screenTree
		if msg.err != nil {
			m.err = msg.err
			m.notice = ""
			break
		}
		m.err = nil
		m.notice = "deleted " + filepath.Base(msg.path)
		m.lines = nil
		m.viewerPath = ""
		m.treeCursor = m.treeIndexForApplication(msg.applicationIndex)
		m.ensureTreeMaps()
		delete(m.filesByApplication, msg.applicationIndex)
		if msg.applicationIndex < 0 || msg.applicationIndex >= len(m.applications) {
			break
		}
		m.loading = true
		return m, loadFiles(msg.applicationIndex, m.applications[msg.applicationIndex])
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
		Empty:       "(no applications)",
		Notice:      m.notice,
		Err:         m.err,
		ConfirmPath: m.confirmPath(),
	}
	return views.RenderLogBrowser(context)
}

func sortApplications(applications []process.ProcessInfo) {
	sort.Slice(applications, func(i, j int) bool {
		left, right := applications[i], applications[j]
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.ID < right.ID
	})
}

func matchingApplication(applications []process.ProcessInfo, target string) int {
	if target == "" {
		return 0
	}
	for index, app := range applications {
		if strconv.Itoa(app.ID) == target ||
			app.Name == target ||
			app.Namespace+":"+app.Name == target ||
			app.Namespace == target {
			return index
		}
	}
	return 0
}

func (m Model) breadcrumb() []string {
	parts := []string{"log files"}
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

func (m *Model) ensureTreeMaps() {
	if m.expanded == nil {
		m.expanded = make(map[int]bool)
	}
	if m.filesByApplication == nil {
		m.filesByApplication = make(map[int][]logfile.FileInfo)
	}
}

func clampIndex(index, length int) int {
	if length == 0 {
		return 0
	}
	return max(0, min(index, length-1))
}

var _ tea.Model = Model{}
