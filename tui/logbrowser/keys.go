package logbrowser

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) handleKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := message.String()
	if key == "q" || key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.screen == screenConfirmDelete {
		switch key {
		case "y", "Y":
			path := m.selectedFilePath()
			if path == "" {
				m.screen = m.confirmReturn
				return m, nil
			}
			m.loading = true
			return m, deleteFile(path)
		case "n", "N", "esc":
			m.screen = m.confirmReturn
			return m, nil
		default:
			return m, nil
		}
	}

	switch key {
	case "esc", "backspace":
		switch m.screen {
		case screenApplications:
			return m, tea.Quit
		case screenFiles:
			m.screen = screenApplications
			m.files = nil
			m.err = nil
		case screenViewer:
			m.screen = screenFiles
			m.lines = nil
			m.err = nil
		}
		return m, nil
	case "up", "k":
		m.moveSelection(-1)
		return m, nil
	case "down", "j":
		m.moveSelection(1)
		return m, nil
	case "home":
		m.moveToBoundary(false)
		return m, nil
	case "end":
		m.moveToBoundary(true)
		return m, nil
	case "enter":
		return m.openSelection()
	case "d":
		if (m.screen == screenFiles || m.screen == screenViewer) && len(m.files) > 0 {
			m.confirmReturn = m.screen
			m.screen = screenConfirmDelete
		}
		return m, nil
	default:
		return m, nil
	}
}

func (m *Model) moveSelection(delta int) {
	switch m.screen {
	case screenApplications:
		m.appSelected = clampIndex(m.appSelected+delta, len(m.applications))
	case screenFiles:
		m.fileSelected = clampIndex(m.fileSelected+delta, len(m.files))
	case screenViewer:
		m.lineCursor = clampIndex(m.lineCursor+delta, len(m.lines))
	}
}

func (m *Model) moveToBoundary(end bool) {
	index := 0
	switch m.screen {
	case screenApplications:
		if end {
			index = len(m.applications) - 1
		}
		m.appSelected = clampIndex(index, len(m.applications))
	case screenFiles:
		if end {
			index = len(m.files) - 1
		}
		m.fileSelected = clampIndex(index, len(m.files))
	case screenViewer:
		if end {
			index = len(m.lines) - 1
		}
		m.lineCursor = clampIndex(index, len(m.lines))
	}
}

func (m Model) openSelection() (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenApplications:
		if len(m.applications) == 0 {
			return m, nil
		}
		m.screen = screenFiles
		m.files = nil
		m.fileSelected = 0
		m.loading = true
		m.err = nil
		m.notice = ""
		return m, loadFiles(m.selectedApplication())
	case screenFiles:
		if len(m.files) == 0 {
			return m, nil
		}
		m.screen = screenViewer
		m.lines = nil
		m.lineCursor = 0
		m.loading = true
		m.err = nil
		m.notice = ""
		return m, loadFile(m.selectedFilePath())
	default:
		return m, nil
	}
}
