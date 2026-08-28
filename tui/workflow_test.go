package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/runhistory"
	"github.com/bizshuk/pm2/workflow"
)

func workflowModel(t *testing.T) Model {
	t.Helper()
	m := New("sock", true)
	m.allProcs = []process.ProcessInfo{
		{AppConfig: process.AppConfig{Name: "a", Namespace: "prod"}, ID: 1},
		{AppConfig: process.AppConfig{Name: "b", Namespace: "dev"}, ID: 2},
	}
	m.workflows = []workflow.Status{
		{Config: workflow.Config{Category: "ci", Name: "build"}},
		{Config: workflow.Config{Category: "ci", Name: "deploy"}},
	}
	m.recomputeNamespaces()
	m.applyNamespaceFilter()
	return m
}

func toWorkflowTab(t *testing.T, m Model) Model {
	t.Helper()
	m.nsCursor = len(m.namespaces) - 1
	m.applyNamespaceFilter()
	if !m.inWorkflowScope() {
		t.Fatalf("setup: expected workflow scope at cursor %d of %v", m.nsCursor, m.namespaces)
	}
	return m
}

// TestWorkflowScopeClearsProcessRows pins why the task keys are safe on
// this tab: there is no selected process to address at all, so a stray
// restart cannot land on whichever row happened to be selected before
// the user switched tabs.
func TestWorkflowScopeClearsProcessRows(t *testing.T) {
	m := toWorkflowTab(t, workflowModel(t))
	if len(m.procs) != 0 {
		t.Errorf("workflow scope still lists %d process rows", len(m.procs))
	}
}

// TestWorkflowTabIgnoresTaskActions confirms r / p / d issue no RPC on
// the workflow tab. A workflow stage never enters the process registry,
// so those verbs have nothing to address.
func TestWorkflowTabIgnoresTaskActions(t *testing.T) {
	for _, key := range []string{"r", "p", "d"} {
		m := toWorkflowTab(t, workflowModel(t))
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if cmd != nil {
			t.Errorf("key %q dispatched a command on the workflow tab", key)
		}
	}
}

// TestWorkflowNavigationUsesItsOwnCursor checks the two selections stay
// independent: coming back to a namespace must not silently re-target
// the task the user had highlighted.
func TestWorkflowNavigationUsesItsOwnCursor(t *testing.T) {
	m := workflowModel(t)
	m.selected = 1
	m = toWorkflowTab(t, m)

	res, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = res.(Model)
	if m.wfSelected != 1 {
		t.Errorf("wfSelected = %d, want 1", m.wfSelected)
	}

	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = res.(Model)
	if m.wfSelected != 1 {
		t.Errorf("wfSelected = %d, want it clamped at the last row", m.wfSelected)
	}

	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = res.(Model)
	if m.wfSelected != 0 {
		t.Errorf("wfSelected = %d, want 0", m.wfSelected)
	}

	m.nsCursor = 0
	m.applyNamespaceFilter()
	if m.selected != 1 {
		t.Errorf("task selection = %d, want the 1 it was left at", m.selected)
	}
}

// TestWorkflowTabSurvivesRefresh keeps the user where they were: the
// chip strip is rebuilt on every refresh, and matching the previous
// chip by name would drop them back to "All" the moment a namespace
// appeared or vanished.
func TestWorkflowTabSurvivesRefresh(t *testing.T) {
	m := toWorkflowTab(t, workflowModel(t))

	updated, _ := m.applyRefresh(refreshMsg{
		procs: []process.ProcessInfo{
			{AppConfig: process.AppConfig{Name: "c", Namespace: "staging"}, ID: 3},
		},
		workflows: m.workflows,
	})
	m = updated.(Model)

	if !m.inWorkflowScope() {
		t.Fatalf("refresh moved the cursor off the workflow tab: %d of %v", m.nsCursor, m.namespaces)
	}
	if m.namespaces[len(m.namespaces)-1] != workflowTab {
		t.Errorf("workflow tab is not last: %v", m.namespaces)
	}
}

// TestWorkflowSelectionClampsOnShrink covers a workflow being deleted
// while its row is selected.
func TestWorkflowSelectionClampsOnShrink(t *testing.T) {
	m := toWorkflowTab(t, workflowModel(t))
	m.wfSelected = 1

	updated, _ := m.applyRefresh(refreshMsg{
		procs: m.allProcs,
		workflows: []workflow.Status{
			{
				Config:     workflow.Config{Category: "ci", Name: "build"},
				LastStatus: runhistory.StatusSuccess,
				LastRunAt:  time.Now(),
			},
		},
	})
	m = updated.(Model)

	if m.wfSelected != 0 {
		t.Errorf("wfSelected = %d, want 0 after the list shrank", m.wfSelected)
	}
}

// TestWorkflowTabRendersWithoutAProcessList is the regression the
// two-pane layout invites: every renderer used to assume Procs was the
// subject, and an empty one meant "nothing to draw".
func TestWorkflowTabRendersWithoutAProcessList(t *testing.T) {
	m := toWorkflowTab(t, workflowModel(t))
	m.width, m.height = 120, 30

	out := m.View()
	for _, want := range []string{"WORKFLOWS", "ci:build", "never run"} {
		if !contains(out, want) {
			t.Errorf("rendered workflow tab missing %q:\n%s", want, out)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		(func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		})()
}
