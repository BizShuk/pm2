package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/sysmon"
	"github.com/bizshuk/pm2/tui/theme"
)

// Dashboard scope labels. The controller passes the active scope as a
// string for the same reason ViewContext takes SortBy as one: views stay
// free of the controller's enum types.
const (
	ScopeTasks  = "tasks"
	ScopeSystem = "system"
)

// dashboardHostRows is the height of the fixed host-resource panel.
const dashboardHostRows = 4

// DashboardContext is everything the activity dashboard needs to draw one
// frame. Like ViewContext it is built fresh by the controller each frame
// and never mutated by a renderer.
//
// Task and Proc are the right-pane subject for their respective scopes;
// exactly one is non-nil whenever the list has a selection. Children and
// Ports belong to whichever is set — the controller resolves them because
// only it holds the process table the walk needs.
type DashboardContext struct {
	Width    int
	Height   int
	Snapshot sysmon.Snapshot
	Procs    []sysmon.Proc // ranked OS process table, system scope only
	Selected int
	Scope    string
	SortBy   string
	Task     *sysmon.Task
	Proc     *sysmon.Proc
	Children []sysmon.Proc
	Ports    []sysmon.Port
	Updated  time.Time
	Err      error
	Notice   string
}

// RenderDashboard is the single entry point the dashboard controller
// calls per frame: title bar, whole-machine resource panel, then a
// two-pane list/detail body over a key legend.
func RenderDashboard(ctx DashboardContext) string {
	if ctx.Width < 60 {
		return "terminal too narrow (min 60 cols)"
	}

	bodyHeight := max(ctx.Height-dashboardHostRows-2, 3) // host panel + header + footer
	leftWidth := dashboardListWidth(ctx.Width)
	rightWidth := ctx.Width - leftWidth - 1

	left := lipgloss.NewStyle().Width(leftWidth).Height(bodyHeight).
		Render(RenderDashboardList(ctx, leftWidth, bodyHeight))
	divider := lipgloss.NewStyle().Width(1).Height(bodyHeight).Foreground(theme.Border).
		Render(strings.Repeat("│\n", bodyHeight-1) + "│")
	right := lipgloss.NewStyle().Width(rightWidth).Height(bodyHeight).
		Render(RenderDashboardDetail(ctx, rightWidth, bodyHeight))

	return lipgloss.JoinVertical(lipgloss.Left,
		renderDashboardHeader(ctx),
		RenderHostPanel(ctx.Snapshot.System, ctx.Width),
		lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right),
		renderDashboardFooter(ctx),
	)
}

// dashboardListWidth keeps the process list wide enough for a name plus
// its CPU and memory cells while leaving the detail pane at least half
// the screen on a normal terminal.
func dashboardListWidth(total int) int {
	return max(min(38, total/2), dashboardRowFixed+dashboardMinLabelW)
}

func renderDashboardHeader(ctx DashboardContext) string {
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.Text).Render("pm2 dashboard")

	var info string
	switch {
	case ctx.Notice != "":
		info = lipgloss.NewStyle().Foreground(theme.Errored).Render("  ✗ " + ctx.Notice)
	case ctx.Updated.IsZero():
		info = lipgloss.NewStyle().Foreground(theme.Muted).Render("  sampling…")
	default:
		host := ctx.Snapshot.Host
		summary := fmt.Sprintf("  %s · %d cores · up %s · %d procs (%d running) · %s",
			host.Hostname,
			host.Cores,
			formatUptimeSeconds(host.UptimeSeconds),
			ctx.Snapshot.Processes.Total,
			ctx.Snapshot.Processes.Running,
			ctx.Updated.Format("15:04:05"),
		)
		info = lipgloss.NewStyle().Foreground(theme.Muted).Render(summary)
	}

	// MaxWidth trims by printable columns; CropRight would count the
	// styling escapes in title+info as visible characters.
	return lipgloss.NewStyle().Background(theme.HdrBg).Width(ctx.Width).
		MaxWidth(ctx.Width).MaxHeight(1).Padding(0, 1).
		Render(title + info)
}

func renderDashboardFooter(ctx DashboardContext) string {
	scope := "all processes"
	if ctx.Scope == ScopeSystem {
		scope = "pm2 tasks"
	}
	keys := [][2]string{
		{"↑↓ / jk", "navigate"},
		{"a", scope},
		{"s", "sort: " + ctx.SortBy},
		{"q", "quit"},
	}

	keyStyle := lipgloss.NewStyle().Foreground(theme.Text)
	descStyle := lipgloss.NewStyle().Foreground(theme.Muted)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, keyStyle.Render(key[0])+" "+descStyle.Render(key[1]))
	}
	return lipgloss.NewStyle().Background(theme.HdrBg).Width(ctx.Width).Padding(0, 1).
		Render(strings.Join(parts, "  │  "))
}

// RenderDashboardList draws the left pane: managed applications in task
// scope, the busiest OS processes in system scope.
func RenderDashboardList(ctx DashboardContext, width, height int) string {
	rows := dashboardRows(ctx, width)
	header := secHeader(dashboardListTitle(ctx), width)

	if len(rows) == 0 {
		empty := "no managed tasks\nrun: pm2 task start ecosystem.config.js"
		if ctx.Scope == ScopeSystem {
			empty = "no processes visible"
		}
		rows = []string{lipgloss.NewStyle().Width(width).Padding(0, 1).
			Foreground(theme.Muted).Render(empty)}
	}

	visible := scrollWindow(rows, ctx.Selected, height-1)
	blank := strings.Repeat(" ", width)
	for len(visible) < height-1 {
		visible = append(visible, blank)
	}
	return header + "\n" + strings.Join(visible, "\n")
}

func dashboardListTitle(ctx DashboardContext) string {
	if ctx.Scope == ScopeSystem {
		return fmt.Sprintf("processes (%d)", len(ctx.Procs))
	}
	return fmt.Sprintf("pm2 tasks (%d)", len(ctx.Snapshot.Tasks))
}

// Fixed cells of a list row: the status marker, the CPU and memory
// columns, the three spaces between the four fields, and the pane's own
// one-column padding on each side. Everything left over is the label.
// Getting this wrong by one column makes lipgloss wrap every row onto two
// lines, which is how the pane silently loses half its entries.
const (
	dashboardCPUWidth  = 6
	dashboardMemWidth  = 7
	dashboardRowFixed  = 1 + 3 + dashboardCPUWidth + dashboardMemWidth + 2
	dashboardPIDWidth  = 6
	dashboardMinLabelW = 6
)

// dashboardRows renders one line per list entry. Both scopes share a
// trailing "cpu% mem" pair so the columns line up when toggling.
func dashboardRows(ctx DashboardContext, width int) []string {
	labelWidth := max(width-dashboardRowFixed, dashboardMinLabelW)

	var rows []string
	if ctx.Scope == ScopeSystem {
		for index, proc := range ctx.Procs {
			name := CropRight(proc.Executable(), max(labelWidth-dashboardPIDWidth-1, 1))
			label := fmt.Sprintf("%*d %s", dashboardPIDWidth, proc.PID, name)
			rows = append(rows, dashboardRow(
				" ", label, proc.CPUPercent, proc.MemoryBytes,
				index == ctx.Selected, width, labelWidth, true,
			))
		}
		return rows
	}

	for index, task := range ctx.Snapshot.Tasks {
		status := process.Status(task.Status)
		rows = append(rows, dashboardRow(
			dotFor(status),
			CropRight(task.Name, labelWidth),
			task.TreeCPUPercent, task.TreeMemoryBytes,
			index == ctx.Selected, width, labelWidth,
			status == process.StatusOnline,
		))
	}
	return rows
}

func dashboardRow(marker, label string, cpu float64, memory uint64, selected bool, width, labelWidth int, active bool) string {
	labelStyle := lipgloss.NewStyle().Foreground(theme.Text).Width(labelWidth)
	metricStyle := lipgloss.NewStyle().Foreground(theme.Muted)
	if active {
		metricStyle = lipgloss.NewStyle().Foreground(theme.Online)
	}
	if selected {
		labelStyle = labelStyle.Bold(true).Foreground(theme.SelName)
		metricStyle = metricStyle.Bold(true)
	}

	line := fmt.Sprintf("%s %s %s %s",
		marker,
		labelStyle.Render(label),
		metricStyle.Width(dashboardCPUWidth).Align(lipgloss.Right).Render(fmt.Sprintf("%.1f%%", cpu)),
		metricStyle.Width(dashboardMemWidth).Align(lipgloss.Right).Render(formatBytes(memory)),
	)

	// MaxHeight(1) is a backstop, not decoration: if the label budget is
	// ever a column too generous, lipgloss wraps the row onto a second
	// line and the pane silently drops half its entries.
	rowStyle := lipgloss.NewStyle().Width(width).MaxHeight(1).Padding(0, 1)
	if selected {
		rowStyle = rowStyle.Background(theme.SelBg)
	}
	return rowStyle.Render(line)
}

// scrollWindow returns the slice of rows containing selected, keeping the
// cursor inside a window of at most height lines. Without it a machine
// with 600 processes would render every one of them and let lipgloss clip
// whichever end it liked.
func scrollWindow(rows []string, selected, height int) []string {
	if height <= 0 || len(rows) <= height {
		return rows
	}
	start := max(selected-height/2, 0)
	if start+height > len(rows) {
		start = len(rows) - height
	}
	return rows[start : start+height]
}
