package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
)

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	if msg.String() == "s" {
		m.cycleSort()
		m.sortProcs()
		return m, nil
	}
	// Namespace switching is intentionally available even when the
	// current filter has zero rows — otherwise a user could get stuck
	// on an empty namespace with no way back.
	switch msg.String() {
	case "left":
		m.cycleNamespace(-1)
		return m, nil
	case "right":
		m.cycleNamespace(+1)
		return m, nil
	}
	if len(m.procs) == 0 {
		return m, nil
	}
	targetID := fmt.Sprintf("%d", m.procs[m.selected].ID)
	switch msg.String() {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
			if m.Detail {
				m.logs = nil
				return m, readLogs(m.procs[m.selected].LogFile)
			}
			return m, nil
		}
	case "down", "j":
		if m.selected < len(m.procs)-1 {
			m.selected++
			if m.Detail {
				m.logs = nil
				return m, readLogs(m.procs[m.selected].LogFile)
			}
			return m, nil
		}
	case "r":
		return m, doAction(m.socket, model.Request{Command: model.CmdRestart, Name: targetID})
	case "p":
		// Toggle pause/resume on the selected process. Pausing a cron
		// task suspends its schedule (status → paused) and stops any
		// running instance; pressing again resumes it (a cron task
		// returns to idle, a regular process comes back online). The
		// selected status was set by the last refresh, and doAction
		// refreshes immediately, so successive presses flip cleanly.
		cmd := pauseOrResume(m.procs[m.selected].Status)
		return m, doAction(m.socket, model.Request{Command: cmd, Name: targetID})
	case "d":
		return m, doAction(m.socket, model.Request{Command: model.CmdDelete, Name: targetID})
	case "enter":
		// Toggle log-focus: in two-pane mode, hide the detail block and
		// show the log tail filling the full right pane. Pressing Enter
		// again restores the detail+logs view. No-op in wide-table mode
		// (where there's no detail block to hide) and on an empty list.
		if m.Detail {
			m.logFocus = !m.logFocus
		}
		return m, nil
	case "esc":
		// Convenience exit from log-focus. No-op in wide-table mode and
		// when log-focus is already off, so it never steals Esc from
		// any other future binding.
		if m.Detail {
			m.logFocus = false
		}
		return m, nil
	}
	return m, nil
}

// pauseOrResume picks the RPC command for the `p` key toggle: a paused
// process resumes, anything else pauses.
func pauseOrResume(s process.Status) model.CommandType {
	if s == process.StatusPaused {
		return model.CmdResume
	}
	return model.CmdPause
}
