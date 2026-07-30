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
			row, ok := m.selectedTreeRow()
			path := m.selectedFilePath()
			if !ok || row.kind != treeFile || path == "" {
				m.screen = screenTree
				return m, nil
			}
			m.loading = true
			return m, deleteFile(row.applicationIndex, path)
		case "n", "N", "esc":
			m.screen = screenTree
			return m, nil
		default:
			return m, nil
		}
	}

	switch key {
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
	case "right":
		return m.moveIn()
	case "left":
		return m.moveOut()
	case "enter":
		return m.focusSelectedFile()
	case "pgup":
		if m.screen == screenViewer {
			m.movePage(-1)
		}
		return m, nil
	case "pgdown":
		if m.screen == screenViewer {
			m.movePage(1)
		}
		return m, nil
	case "d":
		if m.canDelete() {
			m.screen = screenConfirmDelete
		}
		return m, nil
	default:
		return m, nil
	}
}

func (m *Model) moveSelection(delta int) {
	switch m.screen {
	case screenTree:
		m.treeCursor = clampIndex(m.treeCursor+delta, len(m.visibleTreeRows()))
	case screenViewer:
		m.lineCursor = clampIndex(m.lineCursor+delta, len(m.lines))
	}
}

func (m *Model) moveToBoundary(end bool) {
	index := 0
	switch m.screen {
	case screenTree:
		if end {
			index = len(m.visibleTreeRows()) - 1
		}
		m.treeCursor = clampIndex(index, len(m.visibleTreeRows()))
	case screenViewer:
		if end {
			index = len(m.lines) - 1
		}
		m.lineCursor = clampIndex(index, len(m.lines))
	}
}

func (m Model) moveIn() (tea.Model, tea.Cmd) {
	if m.screen != screenTree {
		return m, nil
	}
	row, ok := m.selectedTreeRow()
	if !ok {
		return m, nil
	}
	if row.kind == treeApplication {
		m.ensureTreeMaps()
		if m.expanded[row.applicationIndex] {
			if len(m.filesByApplication[row.applicationIndex]) > 0 {
				m.treeCursor++
			}
			return m, nil
		}
		m.expanded[row.applicationIndex] = true
		if _, loaded := m.filesByApplication[row.applicationIndex]; loaded {
			return m, nil
		}
		m.loading = true
		m.err = nil
		m.notice = ""
		return m, loadFiles(row.applicationIndex, m.applications[row.applicationIndex])
	}

	return m.focusSelectedFile()
}

func (m Model) focusSelectedFile() (tea.Model, tea.Cmd) {
	if m.screen != screenTree {
		return m, nil
	}
	row, ok := m.selectedTreeRow()
	if !ok || row.kind != treeFile {
		return m, nil
	}
	path := m.selectedFilePath()
	if path == "" {
		return m, nil
	}
	m.screen = screenViewer
	m.viewerPath = path
	m.lines = nil
	m.lineCursor = 0
	m.loading = true
	m.err = nil
	m.notice = ""
	return m, loadFile(path)
}

func (m Model) moveOut() (tea.Model, tea.Cmd) {
	if m.screen == screenViewer {
		m.screen = screenTree
		return m, nil
	}
	if m.screen != screenTree {
		return m, nil
	}

	row, ok := m.selectedTreeRow()
	if !ok {
		return m, nil
	}
	switch row.kind {
	case treeApplication:
		if m.expanded[row.applicationIndex] {
			m.expanded[row.applicationIndex] = false
		}
	case treeFile:
		parentIndex := m.treeIndexForApplication(row.applicationIndex)
		m.expanded[row.applicationIndex] = false
		m.treeCursor = parentIndex
	}
	return m, nil
}

func (m *Model) movePage(direction int) {
	pageSize := max(1, m.height-4)
	m.lineCursor = clampIndex(
		m.lineCursor+direction*pageSize,
		len(m.lines),
	)
}
