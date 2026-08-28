package views

import (
	"strings"
	"testing"
	"time"

	"github.com/bizshuk/pm2/runhistory"
	"github.com/bizshuk/pm2/workflow"
)

func sampleWorkflows() []workflow.Status {
	return []workflow.Status{
		{
			Config: workflow.Config{
				Category: "ci",
				Name:     "build",
				Cron:     "0 3 * * *",
				Timeout:  "30m",
				Stages: []workflow.Stage{
					{Name: "unit", Script: "npm", Args: []string{"test"}},
					{Name: "bounce", Task: "Local:API"},
					{Name: "chain", Workflow: "ci:publish"},
				},
			},
			LastStatus: runhistory.StatusSuccess,
			LastRunAt:  time.Now().Add(-2 * time.Hour),
		},
		{
			Config:     workflow.Config{Category: workflow.DefaultCategory, Name: "nightly"},
			Running:    true,
			RunID:      "20260828T030012-a1b2c3",
			LastStatus: runhistory.StatusFailed,
		},
	}
}

// TestWorkflowPaneShowsStateNotUptime pins the column swap: a workflow
// is idle between triggers, so uptime would read "—" on every row
// forever. The last outcome is the thing worth the space.
func TestWorkflowPaneShowsStateNotUptime(t *testing.T) {
	ctx := ViewContext{Width: 120, Height: 30, Workflows: sampleWorkflows(), WfScope: true}
	out := RenderWorkflowPane(ctx, 40, 20)

	for _, want := range []string{"ci:build", "success", "nightly", "running"} {
		if !strings.Contains(out, want) {
			t.Errorf("workflow pane missing %q:\n%s", want, out)
		}
	}
}

// TestWorkflowPaneLabelsDefaultCategoryBare keeps the common case
// readable: "default:nightly" is noise nobody typed.
func TestWorkflowPaneLabelsDefaultCategoryBare(t *testing.T) {
	out := RenderWorkflowPane(
		ViewContext{Workflows: sampleWorkflows(), WfScope: true}, 40, 20)
	if strings.Contains(out, "default:nightly") {
		t.Errorf("default category should not be shown:\n%s", out)
	}
}

// TestWorkflowPaneEmptyState covers a daemon with no workflows at all,
// which is every daemon until someone declares one.
func TestWorkflowPaneEmptyState(t *testing.T) {
	out := RenderWorkflowPane(ViewContext{WfScope: true}, 40, 20)
	if !strings.Contains(out, "no workflows") {
		t.Errorf("empty workflow pane missing its notice:\n%s", out)
	}
}

// TestWorkflowDetailListsStagesInOrder is the pane's whole purpose: a
// workflow's identity is the sequence it runs.
func TestWorkflowDetailListsStagesInOrder(t *testing.T) {
	out := RenderWorkflowDetail(sampleWorkflows()[0], 70)

	for _, want := range []string{"1.", "unit", "npm test", "2.", "bounce", "Local:API", "3.", "ci:publish"} {
		if !strings.Contains(out, want) {
			t.Errorf("workflow detail missing %q:\n%s", want, out)
		}
	}
	for _, kind := range []string{"script", "task", "workflow"} {
		if !strings.Contains(out, kind) {
			t.Errorf("workflow detail missing stage kind %q:\n%s", kind, out)
		}
	}
	if i, j := strings.Index(out, "unit"), strings.Index(out, "bounce"); i > j {
		t.Errorf("stages rendered out of declaration order:\n%s", out)
	}
}

// TestWorkflowDetailNamesTheTriggerWhenUnscheduled documents the state
// the user asked the tab to show: a workflow with no cron is waiting to
// be triggered, not misconfigured.
func TestWorkflowDetailNamesTheTriggerWhenUnscheduled(t *testing.T) {
	out := RenderWorkflowDetail(sampleWorkflows()[1], 70)
	if !strings.Contains(out, "on trigger") {
		t.Errorf("unscheduled workflow should say it waits for a trigger:\n%s", out)
	}
	if !strings.Contains(out, "20260828T030012-a1b2c3") {
		t.Errorf("a running workflow should show its run id:\n%s", out)
	}
}

// TestWorkflowFooterDropsTaskActions keeps the legend honest: those keys
// do nothing on this tab.
func TestWorkflowFooterDropsTaskActions(t *testing.T) {
	out := RenderFooter(ViewContext{Width: 120, SortBy: "name", WfScope: true})
	for _, gone := range []string{"restart", "pause/resume", "delete"} {
		if strings.Contains(out, gone) {
			t.Errorf("workflow footer still advertises %q:\n%s", gone, out)
		}
	}
	if !strings.Contains(out, "navigate") {
		t.Errorf("workflow footer missing navigation hint:\n%s", out)
	}
}

// TestHeaderCountsWorkflowsInScope stops the title bar reporting "0
// processes" on a tab that is not about processes.
func TestHeaderCountsWorkflowsInScope(t *testing.T) {
	ctx := ViewContext{
		Width:     120,
		Updated:   time.Now(),
		Workflows: sampleWorkflows(),
		WfScope:   true,
	}
	out := RenderHeader(ctx)
	if !strings.Contains(out, "2 workflows") {
		t.Errorf("header should count workflows in workflow scope:\n%s", out)
	}
	if strings.Contains(out, "processes") {
		t.Errorf("header should not mention processes in workflow scope:\n%s", out)
	}
}
