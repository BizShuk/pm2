package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/tui/theme"
)

// RenderDetail renders the right-panel detail table for one process.
// Pure function — see ViewContext. Pass the process from ctx.Procs at
// the index ctx.Selected.
func RenderDetail(p process.ProcessInfo, w int) string {
	hdr := secHeader(fmt.Sprintf("detail — %s", p.Name), w)
	kst := lipgloss.NewStyle().Foreground(theme.Muted).Width(18)

	scriptVal := p.Script
	if len(p.Args) > 0 {
		scriptVal += " " + strings.Join(p.Args, " ")
	}

	type row struct{ k, v, sty string }
	rows := []row{
		{"script", Crop(scriptVal, w-21), "path"},
		{"namespace", CropRight(p.Namespace, w-21), ""},
		{"user", CropRight(p.User, w-21), ""},
		{"status", CropRight(string(p.Status), w-21), "status"},
		{"cpu", Crop(fmt.Sprintf("%.1f%%", p.CPU), w-21), "cpu"},
		{"mem", Crop(formatBytes(p.Memory), w-21), "mem"},
		{"uptime", Crop(fullUptime(p), w-21), ""},
		{"started", Crop(fmtTime(p.StartedAt), w-21), ""},
		{"restarts", Crop(fmt.Sprintf("%d / %d max", p.Restarts, p.MaxRestarts), w-21), ""},
		{"cron", Crop(cronExpr(p.Cron), w-21), "cron"},
		{"cron next", Crop(cronNext(p.Cron), w-21), "cron"},
		{"cron_restart", Crop(cronExpr(p.CronRestart), w-21), "cron"},
		{"cron_restart next", Crop(cronNext(p.CronRestart), w-21), "cron"},
		{"last run", "", "last"},
		{"watching", CropRight(formatWatching(p.Watch), w-21), "watching"},
		{"stdout", Crop(p.LogFile, w-21), "path"},
		{"stderr", Crop(p.ErrorFile, w-21), "path"},
	}
	var lines []string
	for _, r := range rows {
		var val string
		switch r.sty {
		case "path":
			val = lipgloss.NewStyle().Foreground(theme.Path).Render(r.v)
		case "cron":
			val = lipgloss.NewStyle().Foreground(theme.Cron).Render(r.v)
		case "last":
			val = cronLastRunStyled(p.LastCronAt, p.LastCronStatus, w-43)
		case "status":
			val = statusLabel(p.Status)
		case "watching":
			if p.Watch {
				val = lipgloss.NewStyle().Foreground(theme.Online).Render(r.v)
			} else {
				val = lipgloss.NewStyle().Foreground(theme.Muted).Render(r.v)
			}
		case "cpu":
			if p.Status == process.StatusOnline {
				val = lipgloss.NewStyle().Foreground(theme.Online).Render(r.v)
			} else {
				val = lipgloss.NewStyle().Foreground(theme.Muted).Render(r.v)
			}
		case "mem":
			if p.Status == process.StatusOnline {
				val = lipgloss.NewStyle().Foreground(theme.Text).Render(r.v)
			} else {
				val = lipgloss.NewStyle().Foreground(theme.Muted).Render(r.v)
			}
		default:
			val = lipgloss.NewStyle().Foreground(theme.Text).Render(r.v)
		}
		lines = append(lines, lipgloss.NewStyle().Width(w).Padding(0, 1).Render(kst.Render(r.k)+" "+val))
	}
	return hdr + "\n" + strings.Join(lines, "\n")
}