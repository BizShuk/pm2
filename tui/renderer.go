package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/bizshuk/pm2/process"
)

// colDef describes a single column in the list view.
type colDef struct {
	name  string
	width int
	align lipgloss.Position
}

// listColumns is the ordered set of columns rendered by buildListTUI.
// The "name" column (index 2) is dynamically resized based on terminal width.
var listColumns = []colDef{
	{"id", 3, lipgloss.Right},
	{"namespace", 10, lipgloss.Left},
	{"name", 12, lipgloss.Left},
	{"version", 8, lipgloss.Left},
	{"pid", 6, lipgloss.Right},
	{"uptime", 8, lipgloss.Right},
	{"↺", 3, lipgloss.Right},
	{"status", 9, lipgloss.Left},
	{"cpu", 6, lipgloss.Right},
	{"mem", 8, lipgloss.Right},
	{"user", 8, lipgloss.Left},
	{"cron", 10, lipgloss.Left},
	{"last exec", 19, lipgloss.Left},
}

// drawBorder renders a top/middle/bottom border line for the given column layout.
func drawBorder(cols []colDef, left, mid, right, fill string) string {
	var parts []string
	for _, col := range cols {
		parts = append(parts, strings.Repeat(fill, col.width+2))
	}
	return left + strings.Join(parts, mid) + right
}

// sepLine colours a border line with the muted-border palette.
func sepLine(s string) string {
	return lipgloss.NewStyle().Foreground(clBorder).Render(s)
}

// getColVal returns the rendered cell text for a given column on a process row.
func getColVal(p process.ProcessInfo, colName string) string {
	switch colName {
	case "id":
		return fmt.Sprintf("%d", p.ID)
	case "namespace":
		return p.Namespace
	case "name":
		return p.Name
	case "version":
		if p.Version == "" {
			return "-"
		}
		return p.Version
	case "pid":
		if p.PID <= 0 {
			return "-"
		}
		return fmt.Sprintf("%d", p.PID)
	case "uptime":
		return shortUptime(p)
	case "↺":
		return fmt.Sprintf("%d", p.Restarts)
	case "status":
		return string(p.Status)
	case "cpu":
		if p.Status != process.StatusOnline {
			return "0.0%"
		}
		return fmt.Sprintf("%.1f%%", p.CPU)
	case "mem":
		if p.Status != process.StatusOnline {
			return "0b"
		}
		return formatBytes(p.Memory)
	case "user":
		if p.User == "" {
			return "-"
		}
		return p.User
	case "cron":
		if p.Cron == "" {
			return "-"
		}
		return p.Cron
	case "last exec":
		if p.LastCronAt.IsZero() {
			return "-"
		}
		res := p.LastCronAt.Format("2006-01-02 15:04:05")
		if p.LastCronStatus != "" {
			res += fmt.Sprintf(" (%s)", p.LastCronStatus)
		}
		return res
	default:
		return ""
	}
}

// buildTitle renders the top title bar with process count / error indicator.
func (m Model) buildTitle() string {
	name := lipgloss.NewStyle().Bold(true).Foreground(clText).Render("pm2 monit")
	var info string
	if m.err != nil {
		info = lipgloss.NewStyle().Foreground(clErrored).Render("  ✗ daemon unreachable")
	} else if !m.updated.IsZero() {
		info = lipgloss.NewStyle().Foreground(clMuted).Render(
			fmt.Sprintf("  %d processes · %s", len(m.procs), m.updated.Format("15:04:05")),
		)
	}
	return lipgloss.NewStyle().Background(clHdrBg).Width(m.width).Padding(0, 1).Render(name + info)
}

// buildLeft renders the process list panel.
func (m Model) buildLeft(w, h int) string {
	hdr := secHeader("processes", w)
	blank := strings.Repeat(" ", w)

	maxUpLen := 1
	for _, p := range m.procs {
		upLen := len(shortUptime(p))
		if upLen > maxUpLen {
			maxUpLen = upLen
		}
	}
	nameW := w - 5 - maxUpLen
	if nameW < 5 {
		nameW = 5
	}

	var rows []string
	for i, p := range m.procs {
		dot := dotFor(p.Status)
		name := cropRight(p.Name, nameW)
		up := shortUptime(p)

		var line string
		if i == m.selected {
			nameSt := lipgloss.NewStyle().Bold(true).Foreground(clSelName)
			upSt := lipgloss.NewStyle().Foreground(clSelText)
			line = fmt.Sprintf("%s %s %s",
				dot,
				nameSt.Width(nameW).Render(name),
				upSt.Render(up),
			)
		} else {
			line = fmt.Sprintf("%s %-*s %s",
				dot, nameW, name,
				lipgloss.NewStyle().Foreground(clMuted).Render(up),
			)
		}
		st := lipgloss.NewStyle().Width(w).Padding(0, 1)
		if i == m.selected {
			st = st.Background(clSelBg)
		}
		rows = append(rows, st.Render(line))
	}
	if len(rows) == 0 {
		rows = append(rows, lipgloss.NewStyle().Width(w).Padding(0, 1).Foreground(clMuted).Render("no processes"))
	}
	for len(rows) < h-1 {
		rows = append(rows, blank)
	}
	return hdr + "\n" + strings.Join(rows[:min(h-1, len(rows))], "\n")
}

// buildRight renders the right panel: detail + logs (when height allows).
func (m Model) buildRight(w, h int) string {
	if len(m.procs) == 0 {
		return lipgloss.NewStyle().Width(w).Padding(1, 2).Foreground(clMuted).
			Render("no processes\nstart one: pm2 start <script>")
	}
	p := m.procs[m.selected]
	detail := m.buildDetail(p, w)
	if h < 20 {
		return detail
	}
	logH := h - detailRows - 3 // detailRows (17) + detail header (1) + divider newline (1) + log header (1) = 20
	logs := m.buildLogs(p.Name, w, logH)
	return detail + "\n" + logs
}

// buildDetail renders the right-panel detail table for one process.
func (m Model) buildDetail(p process.ProcessInfo, w int) string {
	hdr := secHeader(fmt.Sprintf("detail — %s", p.Name), w)
	kst := lipgloss.NewStyle().Foreground(clMuted).Width(18)

	scriptVal := p.Script
	if len(p.Args) > 0 {
		scriptVal += " " + strings.Join(p.Args, " ")
	}

	type row struct{ k, v, sty string }
	rows := []row{
		{"script", crop(scriptVal, w-21), "path"},
		{"namespace", cropRight(p.Namespace, w-21), ""},
		{"user", cropRight(p.User, w-21), ""},
		{"status", cropRight(string(p.Status), w-21), "status"},
		{"cpu", crop(fmt.Sprintf("%.1f%%", p.CPU), w-21), "cpu"},
		{"mem", crop(formatBytes(p.Memory), w-21), "mem"},
		{"uptime", crop(fullUptime(p), w-21), ""},
		{"started", crop(fmtTime(p.StartedAt), w-21), ""},
		{"restarts", crop(fmt.Sprintf("%d / %d max", p.Restarts, p.MaxRestarts), w-21), ""},
		{"cron", crop(cronExpr(p.Cron), w-21), "cron"},
		{"cron next", crop(cronNext(p.Cron), w-21), "cron"},
		{"cron_restart", crop(cronExpr(p.CronRestart), w-21), "cron"},
		{"cron_restart next", crop(cronNext(p.CronRestart), w-21), "cron"},
		{"last run", "", "last"},
		{"watching", cropRight(formatWatching(p.Watch), w-21), "watching"},
		{"stdout", crop(p.LogFile, w-21), "path"},
		{"stderr", crop(p.ErrorFile, w-21), "path"},
	}
	var lines []string
	for _, r := range rows {
		var val string
		switch r.sty {
		case "path":
			val = lipgloss.NewStyle().Foreground(clPath).Render(r.v)
		case "cron":
			val = lipgloss.NewStyle().Foreground(clCron).Render(r.v)
		case "last":
			val = cronLastRunStyled(p.LastCronAt, p.LastCronStatus, w-43)
		case "status":
			val = statusLabel(p.Status)
		case "watching":
			if p.Watch {
				val = lipgloss.NewStyle().Foreground(clOnline).Render(r.v)
			} else {
				val = lipgloss.NewStyle().Foreground(clMuted).Render(r.v)
			}
		case "cpu":
			if p.Status == process.StatusOnline {
				val = lipgloss.NewStyle().Foreground(clOnline).Render(r.v)
			} else {
				val = lipgloss.NewStyle().Foreground(clMuted).Render(r.v)
			}
		case "mem":
			if p.Status == process.StatusOnline {
				val = lipgloss.NewStyle().Foreground(clText).Render(r.v)
			} else {
				val = lipgloss.NewStyle().Foreground(clMuted).Render(r.v)
			}
		default:
			val = lipgloss.NewStyle().Foreground(clText).Render(r.v)
		}
		lines = append(lines, lipgloss.NewStyle().Width(w).Padding(0, 1).Render(kst.Render(r.k)+" "+val))
	}
	return hdr + "\n" + strings.Join(lines, "\n")
}

// buildLogs renders the tail of m.logs inside the right panel.
func (m Model) buildLogs(name string, w, h int) string {
	hdr := secHeader(fmt.Sprintf("logs — %s", name), w)
	blank := strings.Repeat(" ", w)

	var rows []string
	if m.logs == nil {
		rows = append(rows, lipgloss.NewStyle().Width(w).Padding(0, 1).Foreground(clMuted).Render("loading..."))
	} else {
		for _, l := range m.logs {
			rows = append(rows, lipgloss.NewStyle().Width(w).Padding(0, 1).Foreground(clMuted).Render(crop(l, w-3)))
		}
		if len(rows) == 0 {
			rows = append(rows, lipgloss.NewStyle().Width(w).Padding(0, 1).Foreground(clMuted).Render("(no log entries)"))
		}
	}
	for len(rows) < h {
		rows = append(rows, blank)
	}
	visible := rows
	if len(visible) > h {
		visible = visible[len(visible)-h:]
	}
	return hdr + "\n" + strings.Join(visible, "\n")
}

// buildFooter renders the bottom key-binding legend.
func buildFooter(w int, sortBy SortField) string {
	keys := [][2]string{
		{"↑↓ / jk", "navigate"},
		{"r", "restart"},
		{"p", "pause"},
		{"d", "delete"},
		{"s", "sort: " + string(sortBy)},
		{"q", "quit"},
	}
	ks := lipgloss.NewStyle().Foreground(clText)
	ds := lipgloss.NewStyle().Foreground(clMuted)
	var parts []string
	for _, h := range keys {
		parts = append(parts, ks.Render(h[0])+" "+ds.Render(h[1]))
	}
	return lipgloss.NewStyle().Background(clHdrBg).Width(w).Padding(0, 1).
		Render(strings.Join(parts, "  │  "))
}

// getLeftColW picks the left-panel column width so that process rows are
// comfortably padded while leaving at least 40 columns for the right panel.
func (m Model) getLeftColW() int {
	w := 34

	if len(m.procs) > 0 {
		maxNameLen := 0
		maxUpLen := 0
		for _, p := range m.procs {
			if len(p.Name) > maxNameLen {
				maxNameLen = len(p.Name)
			}
			upLen := len(shortUptime(p))
			if upLen > maxUpLen {
				maxUpLen = upLen
			}
		}
		idealW := maxNameLen + maxUpLen + 5
		if idealW > w {
			w = idealW
		}
	}

	minRightW := 40
	maxAllowedW := m.width - minRightW - 1 // 1 for the divider
	if maxAllowedW < 34 {
		maxAllowedW = 34
	}
	if maxAllowedW > 50 {
		maxAllowedW = 50
	}
	if w > maxAllowedW {
		w = maxAllowedW
	}
	if w > m.width-10 {
		w = m.width - 10
	}
	if w < 10 {
		w = 10
	}

	return w
}

// buildListTUI renders the wide table list view used when detail mode is off.
func (m Model) buildListTUI() string {
	if len(m.procs) == 0 {
		body := lipgloss.NewStyle().Width(m.width).Height(m.height-2).
			Align(lipgloss.Center, lipgloss.Center).
			Foreground(clMuted).
			Render("No processes running\nstart one: pm2 start <script>")
		return lipgloss.JoinVertical(lipgloss.Left, m.buildTitle(), body, buildFooter(m.width, m.SortBy))
	}

	width := m.width
	fixedW := 0
	for i, col := range listColumns {
		if i != 2 { // name is index 2
			fixedW += col.width + 3 // width + 2 spaces + 1 separator
		}
	}
	fixedW += 2 // outer borders

	nameW := width - fixedW - 3
	if nameW < 18 {
		nameW = 18
	}

	cols := make([]colDef, len(listColumns))
	copy(cols, listColumns)
	cols[2].width = nameW

	top := drawBorder(cols, "┌", "┬", "┐", "─")

	var hdrParts []string
	hdrStyle := lipgloss.NewStyle().Background(clHdrBg).Foreground(clText).Bold(true)
	for _, col := range cols {
		text := col.name
		if len(text) > col.width {
			text = text[:col.width]
		}
		style := hdrStyle.Width(col.width)
		if col.align == lipgloss.Right {
			style = style.Align(lipgloss.Right)
		} else {
			style = style.Align(lipgloss.Left)
		}
		hdrParts = append(hdrParts, " "+style.Render(text)+" ")
	}
	hdrRow := "│" + strings.Join(hdrParts, "│") + "│"
	sep := drawBorder(cols, "├", "┼", "┤", "─")

	var lines []string
	lines = append(lines, sepLine(top))
	lines = append(lines, sepLine(hdrRow))
	lines = append(lines, sepLine(sep))

	rowStyle := lipgloss.NewStyle()
	borderStyle := lipgloss.NewStyle().Foreground(clBorder)
	for i, p := range m.procs {
		var rowParts []string
		for _, col := range cols {
			val := getColVal(p, col.name)
			if runewidth.StringWidth(val) > col.width {
				val = cropRight(val, col.width)
			}

			style := rowStyle.Width(col.width)
			if col.align == lipgloss.Right {
				style = style.Align(lipgloss.Right)
			} else {
				style = style.Align(lipgloss.Left)
			}

			if i == m.selected {
				style = style.Background(clSelBg)
			}

			var renderedVal string
			if i == m.selected {
				switch col.name {
				case "name":
					renderedVal = style.Bold(true).Foreground(clSelName).Render(val)
				case "id", "status":
					renderedVal = style.Bold(true).Foreground(getStatusColor(p.Status)).Render(val)
				case "cpu":
					cpuSt := style.Bold(true)
					if p.Status == process.StatusOnline {
						cpuSt = cpuSt.Foreground(clOnline)
					} else {
						cpuSt = cpuSt.Foreground(clSelText)
					}
					renderedVal = cpuSt.Render(val)
				case "mem":
					memSt := style.Bold(true)
					if p.Status == process.StatusOnline {
						memSt = memSt.Foreground(clSelText)
					} else {
						memSt = memSt.Foreground(clSelText)
					}
					renderedVal = memSt.Render(val)
				default:
					renderedVal = style.Bold(true).Foreground(clSelText).Render(val)
				}
			} else {
				switch col.name {
				case "id":
					renderedVal = style.Bold(true).Foreground(getStatusColor(p.Status)).Render(val)
				case "status":
					renderedVal = style.Foreground(getStatusColor(p.Status)).Render(val)
				case "cpu":
					cpuSt := style
					if p.Status == process.StatusOnline {
						cpuSt = cpuSt.Foreground(clOnline)
					} else {
						cpuSt = cpuSt.Foreground(clStopped)
					}
					renderedVal = cpuSt.Render(val)
				case "mem":
					memSt := style
					if p.Status == process.StatusOnline {
						memSt = memSt.Foreground(clText)
					} else {
						memSt = memSt.Foreground(clStopped)
					}
					renderedVal = memSt.Render(val)
				default:
					defaultSt := style
					if p.Status != process.StatusOnline && col.name != "name" && col.name != "version" && col.name != "namespace" {
						defaultSt = defaultSt.Foreground(clStopped)
					} else {
						defaultSt = defaultSt.Foreground(clText)
					}
					renderedVal = defaultSt.Render(val)
				}
			}

			var cell string
			if i == m.selected {
				cellSt := lipgloss.NewStyle().Background(clSelBg)
				cell = cellSt.Render(" ") + renderedVal + cellSt.Render(" ")
			} else {
				cell = " " + renderedVal + " "
			}
			rowParts = append(rowParts, cell)
		}

		line := borderStyle.Render("│") + strings.Join(rowParts, borderStyle.Render("│")) + borderStyle.Render("│")
		lines = append(lines, line)
	}

	bottom := drawBorder(cols, "└", "┴", "┘", "─")
	lines = append(lines, sepLine(bottom))

	hostMetricsW := m.width
	cpuMemLine, diskNetLine := m.buildHostMetricsLines(hostMetricsW)
	lines = append(lines, cpuMemLine, diskNetLine)

	contentH := m.height - 2
	for len(lines) < contentH {
		lines = append(lines, strings.Repeat(" ", m.width))
	}
	body := strings.Join(lines[:contentH], "\n")

	return lipgloss.JoinVertical(lipgloss.Left, m.buildTitle(), body, buildFooter(m.width, m.SortBy))
}