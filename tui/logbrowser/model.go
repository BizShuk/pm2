// Package logbrowser owns the interactive application → log file → viewer
// state machine used by the pm2 logs command.
package logbrowser

import (
	"fmt"
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
	screenApplications screen = iota
	screenFiles
	screenViewer
	screenConfirmDelete
)

type applicationsMsg struct {
	applications []process.ProcessInfo
	err          error
}

type filesMsg struct {
	files []logfile.FileInfo
	err   error
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

// Model is the Bubble Tea controller for browsing and deleting managed log
// files.
type Model struct {
	socket        string
	initialTarget string
	screen        screen
	confirmReturn screen
	applications  []process.ProcessInfo
	appSelected   int
	files         []logfile.FileInfo
	fileSelected  int
	lines         []string
	lineCursor    int
	width         int
	height        int
	loading       bool
	err           error
	notice        string
}

// New returns a log browser rooted at the application list. initialTarget may
// be an application ID, name, namespace:name key, or namespace and only affects
// the initially selected row.
func New(socket, initialTarget string) Model {
	return Model{
		socket:        socket,
		initialTarget: initialTarget,
		screen:        screenApplications,
		width:         100,
		height:        30,
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
			m.appSelected = matchingApplication(m.applications, m.initialTarget)
			m.initialTarget = ""
		}
	case filesMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.files = msg.files
			m.fileSelected = clampIndex(m.fileSelected, len(m.files))
		}
	case fileMsg:
		if m.screen != screenViewer || m.selectedFilePath() != msg.path {
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
		if msg.err != nil {
			m.err = msg.err
			m.notice = ""
			m.screen = m.confirmReturn
			break
		}
		m.err = nil
		m.notice = "deleted " + filepath.Base(msg.path)
		m.screen = screenFiles
		m.lines = nil
		m.files = nil
		m.fileSelected = 0
		m.loading = true
		return m, loadFiles(m.selectedApplication())
	}
	return m, nil
}

// View delegates all presentation to the pure tui/views renderer.
func (m Model) View() string {
	context := views.LogBrowserContext{
		Width:       m.width,
		Height:      m.height,
		Breadcrumb:  m.breadcrumb(),
		Selected:    m.selectedIndex(),
		LineCursor:  m.lineCursor,
		Viewer:      m.screen == screenViewer,
		CanDelete:   m.screen == screenFiles,
		Loading:     m.loading,
		Empty:       m.emptyMessage(),
		Notice:      m.notice,
		Err:         m.err,
		ConfirmPath: m.confirmPath(),
	}
	switch m.screen {
	case screenApplications:
		context.Items = applicationRows(m.applications)
	case screenFiles:
		context.Items = fileRows(m.files)
	case screenViewer:
		context.Lines = m.lines
		context.CanDelete = true
	case screenConfirmDelete:
		if m.confirmReturn == screenViewer {
			context.Viewer = true
			context.Lines = m.lines
		} else {
			context.Items = fileRows(m.files)
		}
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

func (m Model) selectedApplication() process.ProcessInfo {
	if len(m.applications) == 0 {
		return process.ProcessInfo{}
	}
	return m.applications[clampIndex(m.appSelected, len(m.applications))]
}

func (m Model) selectedFilePath() string {
	if len(m.files) == 0 {
		return ""
	}
	return m.files[clampIndex(m.fileSelected, len(m.files))].Path
}

func (m Model) selectedIndex() int {
	switch m.screen {
	case screenApplications:
		return m.appSelected
	case screenFiles, screenConfirmDelete:
		return m.fileSelected
	default:
		return 0
	}
}

func (m Model) breadcrumb() []string {
	parts := []string{"applications"}
	if m.screen == screenApplications || len(m.applications) == 0 {
		return parts
	}
	app := m.selectedApplication()
	parts = append(parts, app.Namespace+":"+app.Name, "log files")
	if (m.screen == screenViewer || m.confirmReturn == screenViewer) && len(m.files) > 0 {
		parts = append(parts, filepath.Base(m.selectedFilePath()))
	}
	return parts
}

func (m Model) confirmPath() string {
	if m.screen != screenConfirmDelete {
		return ""
	}
	return m.selectedFilePath()
}

func (m Model) emptyMessage() string {
	switch m.screen {
	case screenApplications:
		return "(no applications)"
	case screenFiles:
		return "(no log files)"
	default:
		return "(empty log file)"
	}
}

func applicationRows(applications []process.ProcessInfo) []string {
	rows := make([]string, len(applications))
	for index, app := range applications {
		namespace := app.Namespace
		if namespace == "" {
			namespace = process.DefaultNamespace
		}
		rows[index] = fmt.Sprintf("%s:%s  id %d  %s", namespace, app.Name, app.ID, app.Status)
	}
	return rows
}

func fileRows(files []logfile.FileInfo) []string {
	rows := make([]string, len(files))
	for index, file := range files {
		kind := "archive"
		if file.Current {
			kind = "current"
		}
		rows[index] = fmt.Sprintf("%-7s  %-28s  %8s  %s",
			kind,
			file.Name,
			formatFileSize(file.Size),
			file.ModTime.Format("2006-01-02 15:04:05"),
		)
	}
	return rows
}

func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	for _, label := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, label)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/unit)
}

func clampIndex(index, length int) int {
	if length == 0 {
		return 0
	}
	return max(0, min(index, length-1))
}

var _ tea.Model = Model{}
