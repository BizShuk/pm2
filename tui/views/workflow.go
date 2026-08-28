package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/bizshuk/pm2/runhistory"
	"github.com/bizshuk/pm2/tui/theme"
	"github.com/bizshuk/pm2/workflow"
)

// RenderWorkflowPane renders the left pane of the workflow tab: one row
// per declared workflow, with its state and when it last ran.
//
// A workflow has no uptime — it is idle by design between triggers — so
// the column that carries a process's uptime carries its last outcome
// instead. That is the number a reader actually wants from something
// that only runs when triggered or scheduled.
func RenderWorkflowPane(ctx ViewContext, w, h int) string {
	hdr := secHeader("workflows", w)
	blank := strings.Repeat(" ", w)

	maxStateLen := 1
	for _, st := range ctx.Workflows {
		if n := len(workflowState(st)); n > maxStateLen {
			maxStateLen = n
		}
	}
	nameW := w - 5 - maxStateLen
	if nameW < 5 {
		nameW = 5
	}

	var rows []string
	for i, wf := range ctx.Workflows {
		dot := workflowDot(wf)
		name := CropRight(workflowLabel(wf), nameW)
		state := workflowState(wf)

		var line string
		if i == ctx.WfSelected {
			nameSt := lipgloss.NewStyle().Bold(true).Foreground(theme.SelName)
			stateSt := lipgloss.NewStyle().Foreground(theme.SelText)
			line = fmt.Sprintf("%s %s %s", dot, nameSt.Width(nameW).Render(name), stateSt.Render(state))
		} else {
			line = fmt.Sprintf("%s %-*s %s", dot, nameW, name,
				lipgloss.NewStyle().Foreground(workflowColour(wf)).Render(state))
		}
		st := lipgloss.NewStyle().Width(w).Padding(0, 1)
		if i == ctx.WfSelected {
			st = st.Background(theme.SelBg)
		}
		rows = append(rows, st.Render(line))
	}
	if len(rows) == 0 {
		rows = append(rows, lipgloss.NewStyle().Width(w).Padding(0, 1).Foreground(theme.Muted).
			Render("no workflows"))
	}
	for len(rows) < h-1 {
		rows = append(rows, blank)
	}
	return hdr + "\n" + strings.Join(rows[:min(h-1, len(rows))], "\n")
}

// RenderWorkflowDetail renders the right pane of the workflow tab: the
// definition, its schedule, and the stages in the order they run.
func RenderWorkflowDetail(wf workflow.Status, w int) string {
	hdr := secHeader(fmt.Sprintf("workflow — %s", workflowLabel(wf)), w)
	kst := lipgloss.NewStyle().Foreground(theme.Muted).Width(18)

	type row struct{ k, v, sty string }
	rows := []row{
		{"category", CropRight(wf.Category, w-21), ""},
		{"name", CropRight(wf.Name, w-21), ""},
		{"state", workflowState(wf), "state"},
		{"stages", fmt.Sprintf("%d", len(wf.Stages)), ""},
		{"trigger", workflowTrigger(wf), "cron"},
		{"cron next", Crop(cronNext(wf.Cron), w-21), "cron"},
		{"timeout", Crop(dashIfEmpty(wf.Timeout), w-21), ""},
		{"last run", Crop(workflowLastRun(wf), w-21), ""},
		{"run id", Crop(dashIfEmpty(wf.RunID), w-21), ""},
		{"cwd", Crop(wf.CWD, w-21), "path"},
	}

	var lines []string
	for _, r := range rows {
		var val string
		switch r.sty {
		case "path":
			val = lipgloss.NewStyle().Foreground(theme.Path).Render(r.v)
		case "cron":
			val = lipgloss.NewStyle().Foreground(theme.Cron).Render(r.v)
		case "state":
			val = lipgloss.NewStyle().Foreground(workflowColour(wf)).Render(r.v)
		default:
			val = lipgloss.NewStyle().Foreground(theme.Text).Render(r.v)
		}
		lines = append(lines, lipgloss.NewStyle().Width(w).Padding(0, 1).Render(kst.Render(r.k)+" "+val))
	}

	lines = append(lines, secHeader("stages", w))
	if len(wf.Stages) == 0 {
		lines = append(lines, lipgloss.NewStyle().Width(w).Padding(0, 1).Foreground(theme.Muted).
			Render("none declared"))
	}
	for i, stage := range wf.Stages {
		numSt := lipgloss.NewStyle().Foreground(theme.Muted).Width(4)
		kindSt := lipgloss.NewStyle().Foreground(theme.Cron).Width(9)
		body := CropRight(stageTarget(stage), max(w-18, 5))
		lines = append(lines, lipgloss.NewStyle().Width(w).Padding(0, 1).Render(
			numSt.Render(fmt.Sprintf("%d.", i+1))+
				kindSt.Render(string(stage.Kind()))+
				lipgloss.NewStyle().Foreground(theme.Text).Render(body)))
	}
	return hdr + "\n" + strings.Join(lines, "\n")
}

// workflowLabel is the identity a reader recognises: the bare name in
// the default category, "category:name" everywhere else.
func workflowLabel(wf workflow.Status) string {
	if wf.Category == "" || wf.Category == workflow.DefaultCategory {
		return wf.Name
	}
	return wf.Category + ":" + wf.Name
}

// workflowState collapses "is it running / how did it last end" into one
// word. "never run" is deliberately distinct from a failure: a workflow
// that has not been triggered yet is not a workflow that went wrong.
func workflowState(wf workflow.Status) string {
	if wf.Running {
		return "running"
	}
	if wf.LastStatus == "" {
		return "never run"
	}
	return string(wf.LastStatus)
}

func workflowColour(wf workflow.Status) lipgloss.AdaptiveColor {
	if wf.Running {
		return theme.Online
	}
	switch wf.LastStatus {
	case runhistory.StatusSuccess:
		return theme.Online
	case runhistory.StatusFailed:
		return theme.Errored
	case runhistory.StatusSkipped, runhistory.StatusCancelled:
		return theme.Warn
	default:
		return theme.Muted
	}
}

func workflowDot(wf workflow.Status) string {
	return lipgloss.NewStyle().Foreground(workflowColour(wf)).Render("●")
}

// workflowTrigger says how this workflow starts. A workflow with no
// cron is not misconfigured — it waits for `pm2 workflow run`, a
// webhook, or another workflow — so the field names that instead of
// showing an empty schedule.
func workflowTrigger(wf workflow.Status) string {
	if wf.Cron == "" {
		return "on trigger"
	}
	return cronExpr(wf.Cron)
}

func workflowLastRun(wf workflow.Status) string {
	if wf.LastRunAt.IsZero() {
		return "—"
	}
	return fmt.Sprintf("%s (%s ago)",
		wf.LastRunAt.Format("2006-01-02 15:04:05"),
		formatUptimeSeconds(int64(time.Since(wf.LastRunAt).Seconds())))
}

func stageTarget(stage workflow.Stage) string {
	if stage.Kind() == workflow.StageScript {
		script := stage.Script
		if len(stage.Args) > 0 {
			script += " " + strings.Join(stage.Args, " ")
		}
		return fmt.Sprintf("%s — %s", stage.Name, script)
	}
	return fmt.Sprintf("%s — %s", stage.Name, stage.Ref())
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
