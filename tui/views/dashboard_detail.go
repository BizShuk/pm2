package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/sysmon"
	"github.com/bizshuk/pm2/tui/theme"
)

// detailKeyWidth aligns the label column of the right-pane parameter list.
const detailKeyWidth = 16

// RenderDashboardDetail draws the right pane: the selected subject's
// parameters, the sub-processes it spawned, and the ports its tree
// listens on.
func RenderDashboardDetail(ctx DashboardContext, width, height int) string {
	switch {
	case ctx.Task != nil:
		return dashboardDetailBody(taskHeading(*ctx.Task), taskFields(*ctx.Task), ctx, width, height)
	case ctx.Proc != nil:
		return dashboardDetailBody(ctx.Proc.Executable(), procFields(*ctx.Proc), ctx, width, height)
	default:
		return lipgloss.NewStyle().Width(width).Padding(1, 2).Foreground(theme.Muted).
			Render("nothing selected")
	}
}

// dashboardDetailBody stitches the three sections and fits them to the
// pane. Children come before ports because a subject with no children
// usually has no ports either, and the reverse order would leave a lone
// heading floating at the top.
//
// The two lists share whatever height is left after the field block, with
// ports guaranteed at least their own share: a desktop app can own a
// hundred descendants, and letting that list run would push the port
// numbers — usually the more useful answer — off the pane entirely.
func dashboardDetailBody(heading string, fields []detailField, ctx DashboardContext, width, height int) string {
	lines := []string{secHeader("detail — "+heading, width)}
	fieldRows := 1
	for _, field := range fields {
		rendered := renderDetailField(field, width)
		lines = append(lines, rendered)
		fieldRows += lipgloss.Height(rendered)
	}

	available := max(height-fieldRows-2, 4) // two blank separators
	portRows := min(max(len(ctx.Ports)+1, 2), available/2)
	childRows := available - portRows

	lines = append(lines, "")
	lines = append(lines, renderChildProcesses(ctx.Children, width, childRows)...)
	lines = append(lines, "")
	lines = append(lines, renderListeningPorts(ctx.Ports, width, portRows)...)

	rendered := strings.Join(lines, "\n")
	physicalLines := strings.Split(rendered, "\n")
	if len(physicalLines) > height {
		physicalLines = physicalLines[:height]
	}
	return strings.Join(physicalLines, "\n")
}

// detailField is one label/value row. style names the palette role rather
// than a colour so the mapping stays in one switch.
type detailField struct {
	label string
	value string
	style string
}

func taskHeading(task sysmon.Task) string {
	return task.Namespace + ":" + task.Name
}

func taskFields(task sysmon.Task) []detailField {
	uptime := "—"
	if !task.StartedAt.IsZero() && process.Status(task.Status) == process.StatusOnline {
		uptime = fullUptimeSince(task.StartedAt)
	}

	return []detailField{
		{"status", task.Status, "status"},
		{"pid", pidLabel(task.PID), ""},
		{"uptime", uptime, ""},
		{"restarts", fmt.Sprintf("%d", task.Restarts), ""},
		{"cpu", fmt.Sprintf("%.1f%%   tree %.1f%%", task.CPUPercent, task.TreeCPUPercent), "metric"},
		{"memory", fmt.Sprintf("%s   tree %s", formatBytes(task.MemoryBytes), formatBytes(task.TreeMemoryBytes)), "metric"},
		{"sub-processes", fmt.Sprintf("%d", len(task.Children)), ""},
		{"listening", fmt.Sprintf("%d", len(task.Ports)), ""},
		{"command", task.Command, "path-wrap"},
	}
}

func procFields(proc sysmon.Proc) []detailField {
	return []detailField{
		{"pid", pidLabel(proc.PID), ""},
		{"parent pid", pidLabel(proc.PPID), ""},
		{"state", proc.State, ""},
		{"cpu", fmt.Sprintf("%.1f%%", proc.CPUPercent), "metric"},
		{"memory", formatBytes(proc.MemoryBytes), "metric"},
		{"command", proc.Command, "path-wrap"},
	}
}

func renderDetailField(field detailField, width int) string {
	if field.style == "path-wrap" {
		return renderWrappedDetailField(field.label, field.value, width, detailKeyWidth, theme.Path)
	}

	keyStyle := lipgloss.NewStyle().Foreground(theme.Muted).Width(detailKeyWidth)
	valueWidth := width - detailKeyWidth - 3

	var value string
	switch field.style {
	case "status":
		value = statusLabel(process.Status(field.value))
	case "path":
		value = lipgloss.NewStyle().Foreground(theme.Path).Render(Crop(field.value, valueWidth))
	case "metric":
		value = lipgloss.NewStyle().Foreground(theme.Online).Render(CropRight(field.value, valueWidth))
	default:
		value = lipgloss.NewStyle().Foreground(theme.Text).Render(CropRight(field.value, valueWidth))
	}
	return lipgloss.NewStyle().Width(width).Padding(0, 1).Render(keyStyle.Render(field.label) + " " + value)
}

// childRowFixed is the width of a sub-process row's leading columns —
// "%6d  %5.1f%%  %8s  " — plus the pane's one-column padding on each
// side. The command gets whatever is left.
const childRowFixed = 6 + 2 + 6 + 2 + 8 + 2 + 2

// renderChildProcesses lists the descendants of the selected subject.
// This is the answer to "what did this task actually start" — a managed
// shell script routinely fans out into the processes doing the real work,
// and they are invisible in the process table pm2 keeps.
//
// rows bounds the section, header included, so a subject with a hundred
// descendants cannot run off the pane.
func renderChildProcesses(children []sysmon.Proc, width, rows int) []string {
	lines := []string{secHeader(fmt.Sprintf("sub-processes (%d)", len(children)), width)}
	if len(children) == 0 {
		return append(lines, mutedRow("none", width))
	}

	commandWidth := max(width-childRowFixed, 8)
	shown, truncated := fit(len(children), rows)
	for _, child := range children[:shown] {
		lines = append(lines, detailRow(theme.Text, width, fmt.Sprintf("%6d  %5.1f%%  %8s  %s",
			child.PID,
			child.CPUPercent,
			formatBytes(child.MemoryBytes),
			CropRight(child.Command, commandWidth),
		)))
	}
	if truncated > 0 {
		lines = append(lines, mutedRow(fmt.Sprintf("… %d more", truncated), width))
	}
	return lines
}

// renderListeningPorts lists every socket the subject's tree accepts on,
// which is how a user maps "this task" to "this URL".
func renderListeningPorts(ports []sysmon.Port, width, rows int) []string {
	lines := []string{secHeader(fmt.Sprintf("listening ports (%d)", len(ports)), width)}
	if len(ports) == 0 {
		return append(lines, mutedRow("none", width))
	}

	shown, truncated := fit(len(ports), rows)
	for _, port := range ports[:shown] {
		lines = append(lines, detailRow(theme.Online, width, fmt.Sprintf("%-4s  %-24s  pid %d",
			port.Protocol,
			CropRight(fmt.Sprintf("%s:%d", port.Address, port.Port), 24),
			port.PID,
		)))
	}
	if truncated > 0 {
		lines = append(lines, mutedRow(fmt.Sprintf("… %d more", truncated), width))
	}
	return lines
}

// fit splits total into what a section can show within rows (the section
// header takes one, and a "… N more" marker takes another) and what is
// left over. A section always shows at least one entry.
func fit(total, rows int) (shown, truncated int) {
	budget := max(rows-1, 1)
	if total <= budget {
		return total, 0
	}
	shown = max(budget-1, 1)
	return shown, total - shown
}

// detailRow renders one fixed-height line. MaxHeight(1) matters here for
// the same reason it does in the list pane: a command line long enough to
// wrap would push every following section down the screen.
func detailRow(colour lipgloss.TerminalColor, width int, text string) string {
	return lipgloss.NewStyle().Width(width).MaxHeight(1).Padding(0, 1).
		Foreground(colour).Render(text)
}

func mutedRow(text string, width int) string {
	return detailRow(theme.Muted, width, text)
}

// pidLabel renders a PID, or an em dash for a process that is not running.
func pidLabel(pid int) string {
	if pid <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d", pid)
}
