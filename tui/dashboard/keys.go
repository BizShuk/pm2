package dashboard

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// pageStep is how far PageUp / PageDown move the cursor. The list pane
// height is not known here (only the renderer computes it), so a fixed
// step keeps the controller free of layout arithmetic.
const pageStep = 10

func (m Model) handleKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	if message.String() == "q" || message.String() == "ctrl+c" {
		return m, tea.Quit
	}

	// A pending confirmation swallows every other key: navigating or
	// re-sorting underneath a prompt that names one specific process would
	// leave the prompt describing a row the cursor has already left.
	if m.confirm != nil {
		switch message.String() {
		case "y", "Y":
			target := *m.confirm
			m.confirm = nil
			return m, target.run(m.socket)
		case "n", "N", "esc":
			m.confirm = nil
		}
		return m, nil
	}

	switch message.String() {
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
		m.rememberSelection()

	case "end", "G":
		m.selected = max(0, m.rowCount()-1)
		m.rememberSelection()

	case "a":
		// Toggling scope resets the cursor: row 3 of the pm2 task list and
		// row 3 of a 600-process table have nothing to do with each other,
		// and carrying the index over would look like a random jump.
		m.scope = m.otherScope()
		m.selected = 0
		m.rememberSelection()

	case "s":
		m.cycleSort()
		m.applySort()
		m.restoreSelection()

	case "d":
		// Arm the confirmation rather than acting: the same keystroke
		// means "stop a managed task" in one scope and "signal an
		// arbitrary OS process" in the other, and neither is undoable.
		target, refusal := m.killTargetForSelection()
		if refusal != "" {
			m.action, m.actionAt = refusal, time.Now()
			return m, nil
		}
		m.confirm = &target
	}
	return m, nil
}

func (m *Model) move(delta int) {
	m.selected += delta
	m.clampSelection()
	m.rememberSelection()
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
