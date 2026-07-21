package views

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"

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

// listColumns is the ordered set of columns rendered by the process table.
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

const (
	listWideWidth   = 100
	listNarrowWidth = 80
)

// ProcessTableOptions controls the non-interactive process table renderer.
// Width follows the current terminal width; NoColor guarantees plain output
// for logs and pipelines even when the surrounding process forces ANSI color.
type ProcessTableOptions struct {
	Width   int
	NoColor bool
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
func sepLine(renderer *lipgloss.Renderer, s string) string {
	return renderer.NewStyle().Foreground(theme.Border).Render(s)
}

// getColVal returns the rendered cell text for a given column on a process row.
func getColVal(p process.ProcessInfo, colName string) string {
	switch colName {
	case "id":
		return fmt.Sprintf("%d", p.ID)
	case "namespace":
		return process.NamespaceOrDefault(p.Namespace)
	case "name":
		return p.Name
	case "version":
		return process.VersionOrDash(p.Version)
	case "pid":
		return process.PIDOrDash(p.PID)
	case "uptime":
		return process.ShortUptime(p)
	case "↺":
		return fmt.Sprintf("%d", p.Restarts)
	case "status":
		return string(p.Status)
	case "cpu":
		return process.CPUPercent(p)
	case "mem":
		return process.MemCell(p)
	case "user":
		return process.UserOrDash(p.User)
	case "cron":
		return process.CronOrDash(p.Cron)
	case "last exec":
		return process.LastExec(p)
	default:
		return ""
	}
}

// RenderProcessTable renders a one-shot process snapshot using the bordered,
// status-coloured style formerly shown by the wide `pm2 m` view. It excludes
// interactive dashboard chrome so callers can safely print it to stdout.
func RenderProcessTable(procs []process.ProcessInfo, opts ProcessTableOptions) string {
	renderer := lipgloss.DefaultRenderer()
	if opts.NoColor {
		renderer = lipgloss.NewRenderer(io.Discard, termenv.WithProfile(termenv.Ascii))
	}
	return renderProcessTable(renderer, procs, opts.Width, -1)
}

// processTableColumns returns a copy of the table columns appropriate for the
// terminal width. Tail metadata disappears first; version follows on narrow
// terminals while the core runtime columns remain visible.
func processTableColumns(width int) []colDef {
	cols := append([]colDef(nil), listColumns...)
	if width > 0 && width < listWideWidth {
		cols = withoutColumns(cols, "user", "cron", "last exec")
	}
	if width > 0 && width < listNarrowWidth {
		cols = withoutColumns(cols, "version")
	}
	return cols
}

func withoutColumns(cols []colDef, names ...string) []colDef {
	drop := make(map[string]struct{}, len(names))
	for _, name := range names {
		drop[name] = struct{}{}
	}

	filtered := make([]colDef, 0, len(cols)-len(drop))
	for _, col := range cols {
		if _, ok := drop[col.name]; !ok {
			filtered = append(filtered, col)
		}
	}
	return filtered
}

func renderProcessTable(renderer *lipgloss.Renderer, procs []process.ProcessInfo, width, selected int) string {
	cols := processTableColumns(width)

	nameIndex := -1
	fixedWidth := 3*len(cols) + 1 // cell padding + borders, excluding the name value
	for i, col := range cols {
		if col.name == "name" {
			nameIndex = i
			continue
		}
		fixedWidth += col.width
	}
	if nameIndex >= 0 {
		nameWidth := width - fixedWidth
		if nameWidth < 18 {
			nameWidth = 18
		}
		cols[nameIndex].width = nameWidth
	}

	top := drawBorder(cols, "┌", "┬", "┐", "─")
	headerStyle := renderer.NewStyle().Background(theme.HdrBg).Foreground(theme.Text).Bold(true)
	headerParts := make([]string, 0, len(cols))
	for _, col := range cols {
		label := col.name
		if runewidth.StringWidth(label) > col.width {
			label = CropRight(label, col.width)
		}
		style := headerStyle.Width(col.width)
		if col.align == lipgloss.Right {
			style = style.Align(lipgloss.Right)
		} else {
			style = style.Align(lipgloss.Left)
		}
		headerParts = append(headerParts, " "+style.Render(label)+" ")
	}
	headerRow := "│" + strings.Join(headerParts, "│") + "│"
	separator := drawBorder(cols, "├", "┼", "┤", "─")

	lines := []string{
		sepLine(renderer, top),
		sepLine(renderer, headerRow),
		sepLine(renderer, separator),
	}

	rowStyle := renderer.NewStyle()
	borderStyle := renderer.NewStyle().Foreground(theme.Border)
	for i, p := range procs {
		isSelected := selected >= 0 && i == selected
		rowParts := make([]string, 0, len(cols))
		for _, col := range cols {
			value := getColVal(p, col.name)
			if runewidth.StringWidth(value) > col.width {
				value = CropRight(value, col.width)
			}

			style := rowStyle.Width(col.width)
			if col.align == lipgloss.Right {
				style = style.Align(lipgloss.Right)
			} else {
				style = style.Align(lipgloss.Left)
			}
			if isSelected {
				style = style.Background(theme.SelBg)
			}

			renderedValue := renderProcessCell(style, p, col.name, value, isSelected)
			if isSelected {
				cellStyle := renderer.NewStyle().Background(theme.SelBg)
				rowParts = append(rowParts, cellStyle.Render(" ")+renderedValue+cellStyle.Render(" "))
			} else {
				rowParts = append(rowParts, " "+renderedValue+" ")
			}
		}

		line := borderStyle.Render("│") + strings.Join(rowParts, borderStyle.Render("│")) + borderStyle.Render("│")
		lines = append(lines, line)
	}

	bottom := drawBorder(cols, "└", "┴", "┘", "─")
	lines = append(lines, sepLine(renderer, bottom))
	return strings.Join(lines, "\n")
}

func renderProcessCell(style lipgloss.Style, p process.ProcessInfo, column, value string, selected bool) string {
	if selected {
		switch column {
		case "name":
			return style.Bold(true).Foreground(theme.SelName).Render(value)
		case "id", "status":
			return style.Bold(true).Foreground(getStatusColor(p.Status)).Render(value)
		case "cpu":
			cellStyle := style.Bold(true).Foreground(theme.SelText)
			if p.Status == process.StatusOnline {
				cellStyle = cellStyle.Foreground(theme.Online)
			}
			return cellStyle.Render(value)
		default:
			return style.Bold(true).Foreground(theme.SelText).Render(value)
		}
	}

	switch column {
	case "id":
		return style.Bold(true).Foreground(getStatusColor(p.Status)).Render(value)
	case "status":
		return style.Foreground(getStatusColor(p.Status)).Render(value)
	case "cpu":
		cellStyle := style.Foreground(theme.Stopped)
		if p.Status == process.StatusOnline {
			cellStyle = cellStyle.Foreground(theme.Online)
		}
		return cellStyle.Render(value)
	case "mem":
		cellStyle := style.Foreground(theme.Stopped)
		if p.Status == process.StatusOnline {
			cellStyle = cellStyle.Foreground(theme.Text)
		}
		return cellStyle.Render(value)
	default:
		if p.Status != process.StatusOnline && column != "name" && column != "version" && column != "namespace" {
			style = style.Foreground(theme.Stopped)
		} else {
			style = style.Foreground(theme.Text)
		}
		return style.Render(value)
	}
}

// RenderWideTable renders the wide-table list view used when Detail is
// off. Pure function — see ViewContext.
func RenderWideTable(ctx ViewContext) string {
	if len(ctx.Procs) == 0 {
		body := lipgloss.NewStyle().Width(ctx.Width).Height(ctx.Height-3).
			Align(lipgloss.Center, lipgloss.Center).
			Foreground(theme.Muted).
			Render("No processes running\nstart one: pm2 start <script>")
		return lipgloss.JoinVertical(lipgloss.Left,
			RenderHeader(ctx),
			RenderNamespaceBar(ctx, ctx.Width),
			body,
			RenderFooter(ctx.Width, ctx.SortBy))
	}

	lines := strings.Split(
		renderProcessTable(lipgloss.DefaultRenderer(), ctx.Procs, ctx.Width, ctx.Selected),
		"\n",
	)

	cpuMemLine, diskNetLine := RenderHostMetricsLines(ctx)
	lines = append(lines, cpuMemLine, diskNetLine)

	contentH := ctx.Height - 3
	for len(lines) < contentH {
		lines = append(lines, strings.Repeat(" ", ctx.Width))
	}
	body := strings.Join(lines[:contentH], "\n")

	return lipgloss.JoinVertical(lipgloss.Left,
		RenderHeader(ctx),
		RenderNamespaceBar(ctx, ctx.Width),
		body,
		RenderFooter(ctx.Width, ctx.SortBy))
}
