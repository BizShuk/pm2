package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/tui/theme"
)

// ─── left-column (two-pane) list ─────────────────────────────────────────────

// RenderLeftPane renders the left pane of the two-pane detail view:
// process name + status dot + short uptime. Pure function — see
// ViewContext.
func RenderLeftPane(ctx ViewContext, w, h int) string {
	hdr := secHeader("processes", w)
	blank := strings.Repeat(" ", w)

	maxUpLen := 1
	for _, p := range ctx.Procs {
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
	for i, p := range ctx.Procs {
		dot := dotFor(p.Status)
		name := CropRight(p.Name, nameW)
		up := shortUptime(p)

		var line string
		if i == ctx.Selected {
			nameSt := lipgloss.NewStyle().Bold(true).Foreground(theme.SelName)
			upSt := lipgloss.NewStyle().Foreground(theme.SelText)
			line = fmt.Sprintf("%s %s %s",
				dot,
				nameSt.Width(nameW).Render(name),
				upSt.Render(up),
			)
		} else {
			line = fmt.Sprintf("%s %-*s %s",
				dot, nameW, name,
				lipgloss.NewStyle().Foreground(theme.Muted).Render(up),
			)
		}
		st := lipgloss.NewStyle().Width(w).Padding(0, 1)
		if i == ctx.Selected {
			st = st.Background(theme.SelBg)
		}
		rows = append(rows, st.Render(line))
	}
	if len(rows) == 0 {
		rows = append(rows, lipgloss.NewStyle().Width(w).Padding(0, 1).Foreground(theme.Muted).Render("no processes"))
	}
	for len(rows) < h-1 {
		rows = append(rows, blank)
	}
	return hdr + "\n" + strings.Join(rows[:min(h-1, len(rows))], "\n")
}

// ─── wide-table list ─────────────────────────────────────────────────────────

// colDef describes a single column in the list view.
type colDef struct {
	name  string
	width int
	align lipgloss.Position
}

// listColumns is the ordered set of columns rendered by RenderWideTable.
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
	return lipgloss.NewStyle().Foreground(theme.Border).Render(s)
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

// RenderWideTable renders the wide-table list view used when Detail is
// off. Pure function — see ViewContext.
func RenderWideTable(ctx ViewContext) string {
	if len(ctx.Procs) == 0 {
		body := lipgloss.NewStyle().Width(ctx.Width).Height(ctx.Height-2).
			Align(lipgloss.Center, lipgloss.Center).
			Foreground(theme.Muted).
			Render("No processes running\nstart one: pm2 start <script>")
		return lipgloss.JoinVertical(lipgloss.Left,
			RenderHeader(ctx), body, RenderFooter(ctx.Width, ctx.SortBy))
	}

	width := ctx.Width
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
	hdrStyle := lipgloss.NewStyle().Background(theme.HdrBg).Foreground(theme.Text).Bold(true)
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
	borderStyle := lipgloss.NewStyle().Foreground(theme.Border)
	for i, p := range ctx.Procs {
		var rowParts []string
		for _, col := range cols {
			val := getColVal(p, col.name)
			if runewidth.StringWidth(val) > col.width {
				val = CropRight(val, col.width)
			}

			style := rowStyle.Width(col.width)
			if col.align == lipgloss.Right {
				style = style.Align(lipgloss.Right)
			} else {
				style = style.Align(lipgloss.Left)
			}

			if i == ctx.Selected {
				style = style.Background(theme.SelBg)
			}

			var renderedVal string
			if i == ctx.Selected {
				switch col.name {
				case "name":
					renderedVal = style.Bold(true).Foreground(theme.SelName).Render(val)
				case "id", "status":
					renderedVal = style.Bold(true).Foreground(getStatusColor(p.Status)).Render(val)
				case "cpu":
					cpuSt := style.Bold(true)
					if p.Status == process.StatusOnline {
						cpuSt = cpuSt.Foreground(theme.Online)
					} else {
						cpuSt = cpuSt.Foreground(theme.SelText)
					}
					renderedVal = cpuSt.Render(val)
				case "mem":
					memSt := style.Bold(true)
					if p.Status == process.StatusOnline {
						memSt = memSt.Foreground(theme.SelText)
					} else {
						memSt = memSt.Foreground(theme.SelText)
					}
					renderedVal = memSt.Render(val)
				default:
					renderedVal = style.Bold(true).Foreground(theme.SelText).Render(val)
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
						cpuSt = cpuSt.Foreground(theme.Online)
					} else {
						cpuSt = cpuSt.Foreground(theme.Stopped)
					}
					renderedVal = cpuSt.Render(val)
				case "mem":
					memSt := style
					if p.Status == process.StatusOnline {
						memSt = memSt.Foreground(theme.Text)
					} else {
						memSt = memSt.Foreground(theme.Stopped)
					}
					renderedVal = memSt.Render(val)
				default:
					defaultSt := style
					if p.Status != process.StatusOnline && col.name != "name" && col.name != "version" && col.name != "namespace" {
						defaultSt = defaultSt.Foreground(theme.Stopped)
					} else {
						defaultSt = defaultSt.Foreground(theme.Text)
					}
					renderedVal = defaultSt.Render(val)
				}
			}

			var cell string
			if i == ctx.Selected {
				cellSt := lipgloss.NewStyle().Background(theme.SelBg)
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

	cpuMemLine, diskNetLine := RenderHostMetricsLines(ctx)
	lines = append(lines, cpuMemLine, diskNetLine)

	contentH := ctx.Height - 2
	for len(lines) < contentH {
		lines = append(lines, strings.Repeat(" ", ctx.Width))
	}
	body := strings.Join(lines[:contentH], "\n")

	return lipgloss.JoinVertical(lipgloss.Left,
		RenderHeader(ctx), body, RenderFooter(ctx.Width, ctx.SortBy))
}