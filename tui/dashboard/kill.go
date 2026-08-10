package dashboard

import (
	"fmt"
	"os"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/pm2/model"
)

// killResultMsg reports what the `d` key actually did. It never carries a
// fresh observation: Update re-arms exactly one collection chain from
// observationMsg, and refreshing here would start a second one that runs
// forever alongside the first.
type killResultMsg struct {
	notice string
}

// killTarget is the subject of a pending `d` confirmation.
//
// The two scopes need different verbs. A managed task is owned by the
// daemon, and signalling its PID directly would only hand it to the
// auto-restart loop — the process dies and comes straight back — so the
// task scope asks the daemon to stop it, the same path `pm2 task stop`
// takes. An OS process has no owner here, so it gets a signal.
type killTarget struct {
	system bool
	id     string // daemon target (the task's numeric id), task scope only
	pid    int
	label  string
}

// prompt is the confirmation line. Naming both the verb and the subject
// matters: `d` on a 600-row process table is one keystroke away from
// signalling something the user never meant to select.
func (k killTarget) prompt() string {
	if k.system {
		return fmt.Sprintf("kill %s (pid %d)? — y to confirm, n to cancel", k.label, k.pid)
	}
	return fmt.Sprintf("stop task %s (pid %d)? — y to confirm, n to cancel", k.label, k.pid)
}

func (k killTarget) run(socket string) tea.Cmd {
	if k.system {
		return signalProcess(k.pid, k.label)
	}
	return stopTask(socket, k.id, k.label)
}

// killTargetForSelection resolves the highlighted row, or reports why it
// cannot be killed. A refusal is a notice, not silence — a `d` that does
// nothing at all reads as a broken key.
func (m Model) killTargetForSelection() (killTarget, string) {
	if m.scope == ScopeSystem {
		if m.selected < 0 || m.selected >= len(m.ranked) {
			return killTarget{}, "no process selected"
		}
		proc := m.ranked[m.selected]
		switch {
		case proc.PID == os.Getpid():
			return killTarget{}, "refusing to kill the taskmanager itself"
		case proc.PID <= 1:
			return killTarget{}, "refusing to signal pid 1"
		}
		return killTarget{system: true, pid: proc.PID, label: proc.Executable()}, ""
	}

	tasks := m.observation.Snapshot.Tasks
	if m.selected < 0 || m.selected >= len(tasks) {
		return killTarget{}, "no task selected"
	}
	task := tasks[m.selected]
	if task.PID == 0 {
		return killTarget{}, fmt.Sprintf("%s is not running", task.Name)
	}
	return killTarget{id: fmt.Sprint(task.ID), pid: task.PID, label: task.Name}, ""
}

// stopTask asks the daemon to stop a managed task. It uses model.SendRequest
// rather than the cmd/runtime client for the same reason the collection pass
// does: an observer must never spawn the daemon it is observing.
func stopTask(socket, target, label string) tea.Cmd {
	return func() tea.Msg {
		response, err := model.SendRequest(socket, model.Request{Command: model.CmdStop, Name: target})
		switch {
		case err != nil:
			return killResultMsg{notice: fmt.Sprintf("stop %s failed: %v", label, err)}
		case response != nil && !response.OK:
			return killResultMsg{notice: fmt.Sprintf("stop %s failed: %s", label, response.Error)}
		}
		return killResultMsg{notice: "stopped " + label}
	}
}

// signalProcess sends SIGTERM and stops there. Escalating to SIGKILL is
// the daemon executor's job for processes it owns; for a stranger's
// process the dashboard asks once and lets the next refresh show whether
// it went away.
func signalProcess(pid int, label string) tea.Cmd {
	return func() tea.Msg {
		process, err := os.FindProcess(pid)
		if err != nil {
			return killResultMsg{notice: fmt.Sprintf("kill %d failed: %v", pid, err)}
		}
		if err := process.Signal(syscall.SIGTERM); err != nil {
			return killResultMsg{notice: fmt.Sprintf("kill %d failed: %v", pid, err)}
		}
		return killResultMsg{notice: fmt.Sprintf("sent SIGTERM to %s (pid %d)", label, pid)}
	}
}
