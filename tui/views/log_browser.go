package views

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/bizshuk/pm2/tui/theme"
)

// LogBrowserContext is the complete immutable rendering input for the managed
// log Tree Explorer and Viewer.
type LogBrowserContext struct {
	Width       int
	Height      int
	Breadcrumb  []string
	Items       []string
	Selected    int
	Lines       []string
	LineCursor  int
	ViewerPath  string
	Viewer      bool
	CanDelete   bool
	Loading     bool
	Empty       string
	Notice      string
	Err         error
	ConfirmPath string
}

// RenderLogBrowser renders the dedicated pm2 logs TUI without mutating its
// controller state.
func RenderLogBrowser(ctx LogBrowserContext) string {
	width := max(ctx.Width, 30)
	height := max(ctx.Height, 6)
	header := renderLogBrowserHeader(ctx, width)
	breadcrumb := renderLogBrowserBreadcrumb(ctx.Breadcrumb, width)
	bodyHeight := max(1, height-3)

	var body string
	if ctx.ConfirmPath != "" {
		body = renderDeleteConfirmation(ctx.ConfirmPath, width, bodyHeight)
	} else {
		body = renderLogBrowserSplitPanes(ctx, width, bodyHeight)
	}
	footer := renderLogBrowserFooter(ctx, width)
	return strings.Join([]string{header, breadcrumb, body, footer}, "\n")
}

func renderLogBrowserHeader(ctx LogBrowserContext, width int) string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.Text).
		Render("pm2 logs monitor")
	status := ""
	switch {
	case ctx.Notice != "":
		status = lipgloss.NewStyle().Foreground(theme.Warn).Render("  " + ctx.Notice)
	case ctx.Err != nil:
		status = lipgloss.NewStyle().Foreground(theme.Errored).Render("  error")
	case ctx.Loading:
		status = lipgloss.NewStyle().Foreground(theme.Muted).Render("  loading...")
	}
	return lipgloss.NewStyle().
		Background(theme.HdrBg).
		Width(width).
		Padding(0, 1).
		Render(title + status)
}

func renderLogBrowserBreadcrumb(parts []string, width int) string {
	label := "log files"
	if len(parts) > 0 {
		label = strings.Join(parts, " → ")
	}
	return lipgloss.NewStyle().
		Foreground(theme.Muted).
		Width(width).
		Padding(0, 1).
		Render(CropRight(label, width-2))
}

func renderLogBrowserSplitPanes(ctx LogBrowserContext, width, height int) string {
	leftWidth := max(14, width*2/5)
	leftWidth = min(leftWidth, width-15)
	rightWidth := width - leftWidth - 1
	contentHeight := max(1, height-1)

	left := strings.Join([]string{
		renderLogBrowserPaneTitle("TREE", leftWidth, !ctx.Viewer),
		renderLogBrowserItems(ctx.Items, ctx.Selected, ctx.Empty, leftWidth, contentHeight),
	}, "\n")

	rightTitle := "LOG"
	if ctx.ViewerPath != "" {
		rightTitle += " " + filepath.Base(ctx.ViewerPath)
	}
	var rightBody string
	switch {
	case ctx.Loading:
		rightBody = renderLogBrowserMessage("loading...", rightWidth, contentHeight)
	case ctx.Err != nil:
		rightBody = renderLogBrowserMessage(ctx.Err.Error(), rightWidth, contentHeight)
	case ctx.ViewerPath == "":
		rightBody = renderLogBrowserMessage(
			"Select a log file and press Enter",
			rightWidth,
			contentHeight,
		)
	default:
		rightBody = renderLogViewer(ctx.Lines, ctx.LineCursor, rightWidth, contentHeight)
	}
	right := strings.Join([]string{
		renderLogBrowserPaneTitle(rightTitle, rightWidth, ctx.Viewer),
		rightBody,
	}, "\n")

	dividerRows := make([]string, height)
	for index := range dividerRows {
		dividerRows[index] = "│"
	}
	divider := lipgloss.NewStyle().
		Foreground(theme.Border).
		Render(strings.Join(dividerRows, "\n"))
	return lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)
}

func renderLogBrowserPaneTitle(label string, width int, focused bool) string {
	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.Muted).
		Width(width)
	if focused {
		style = style.
			Background(theme.SelBg).
			Foreground(theme.SelText)
	}
	return style.Render(CropRight(" "+label, width))
}

func renderLogBrowserItems(items []string, selected int, empty string, width, height int) string {
	if len(items) == 0 {
		if empty == "" {
			empty = "(no entries)"
		}
		return renderLogBrowserMessage(empty, width, height)
	}
	selected = max(0, min(selected, len(items)-1))
	start := visibleStart(selected, len(items), height)
	rows := make([]string, 0, height)
	for index := start; index < len(items) && len(rows) < height; index++ {
		marker := "  "
		style := lipgloss.NewStyle().Foreground(theme.Text)
		if index == selected {
			marker = "› "
			style = style.Background(theme.SelBg).Foreground(theme.SelText)
		}
		row := marker + CropRight(items[index], max(0, width-2))
		rows = append(rows, style.Width(width).Render(row))
	}
	return padLogBrowserRows(rows, width, height)
}

func renderLogViewer(lines []string, cursor, width, height int) string {
	if len(lines) == 0 {
		return renderLogBrowserMessage("(empty log file)", width, height)
	}
	cursor = max(0, min(cursor, len(lines)-1))
	start := visibleStart(cursor, len(lines), height)
	rows := make([]string, 0, height)
	for index := start; index < len(lines) && len(rows) < height; index++ {
		marker := "  "
		style := lipgloss.NewStyle().Foreground(theme.Muted)
		if index == cursor {
			marker = "› "
			style = style.Background(theme.SelBg).Foreground(theme.SelText)
		}
		lineNumber := fmt.Sprintf("%6d ", index+1)
		row := marker + lineNumber + CropRight(lines[index], width-11)
		rows = append(rows, style.Width(width).Render(row))
	}
	return padLogBrowserRows(rows, width, height)
}

func renderDeleteConfirmation(path string, width, height int) string {
	const (
		prefix = "Delete "
		suffix = "? [y/N]"
	)
	pathWidth := max(5, width-2-len(prefix)-len(suffix))
	message := prefix + Crop(path, pathWidth) + suffix
	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.Errored).
		Width(width).
		Padding(1, 1)
	rows := []string{style.Render(message)}
	return padLogBrowserRows(rows, width, height)
}

func renderLogBrowserMessage(message string, width, height int) string {
	row := lipgloss.NewStyle().
		Foreground(theme.Muted).
		Width(width).
		Render(" " + CropRight(message, max(0, width-1)))
	return padLogBrowserRows([]string{row}, width, height)
}

func renderLogBrowserFooter(ctx LogBrowserContext, width int) string {
	var hints []string
	switch {
	case ctx.ConfirmPath != "":
		hints = []string{"y confirm delete", "n/esc cancel"}
	case ctx.Viewer:
		hints = []string{
			"↑↓ / jk line",
			"PgUp/PgDn page",
			"← focus tree",
			"q quit",
		}
	default:
		hints = []string{
			"↑↓ / jk navigate",
			"← collapse/back",
			"→ expand/open",
			"Enter focus log",
		}
		if ctx.CanDelete {
			hints = append(hints, "d delete")
		}
		hints = append(hints, "q quit")
	}
	return lipgloss.NewStyle().
		Background(theme.HdrBg).
		Foreground(theme.Muted).
		Width(width).
		Padding(0, 1).
		Render(CropRight(strings.Join(hints, " │ "), width-2))
}

func visibleStart(selected, total, height int) int {
	if total <= height {
		return 0
	}
	start := selected - height + 1
	return max(0, min(start, total-height))
}

func padLogBrowserRows(rows []string, width, height int) string {
	blank := strings.Repeat(" ", width)
	for len(rows) < height {
		rows = append(rows, blank)
	}
	if len(rows) > height {
		rows = rows[:height]
	}
	return strings.Join(rows, "\n")
}
