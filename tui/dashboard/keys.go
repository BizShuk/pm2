package dashboard

import (
	tea "github.com/charmbracelet/bubbletea"
)

// pageStep is how far PageUp / PageDown move the cursor. The list pane
// height is not known here (only the renderer computes it), so a fixed
// step keeps the controller free of layout arithmetic.
const pageStep = 10

func (m Model) handleKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch message.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "up", "k":
		m.move(-1)

	case "down", "j":
		m.move(+1)

	case "pgup":
		m.move(-pageStep)

	case "pgdown":
		m.move(+pageStep)

	case "home", "g":
		m.selected = 0

	case "end", "G":
		m.selected = max(0, m.rowCount()-1)

	case "a":
		// Toggling scope resets the cursor: row 3 of the pm2 task list and
		// row 3 of a 600-process table have nothing to do with each other,
		// and carrying the index over would look like a random jump.
		m.scope = m.otherScope()
		m.selected = 0

	case "s":
		m.cycleSort()
		m.applySort()
		m.clampSelection()
	}
	return m, nil
}

func (m *Model) move(delta int) {
	m.selected += delta
	m.clampSelection()
}

func (m Model) otherScope() Scope {
	if m.scope == ScopeTasks {
		return ScopeSystem
	}
	return ScopeTasks
}

func (m *Model) cycleSort() {
	switch m.sortBy {
	case SortByCPU:
		m.sortBy = SortByMemory
	case SortByMemory:
		m.sortBy = SortByName
	default:
		m.sortBy = SortByCPU
	}
}
