package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/bizshuk/pm2/tui/theme"
)

// LogBrowserContext is the complete immutable rendering input for the
// application → file → viewer log browser.
type LogBrowserContext struct {
	Width       int
	Height      int
	Breadcrumb  []string
	Items       []string
	Selected    int
	Lines       []string
	LineCursor  int
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
	switch {
	case ctx.ConfirmPath != "":
		body = renderDeleteConfirmation(ctx.ConfirmPath, width, bodyHeight)
	case ctx.Loading:
		body = renderLogBrowserMessage("loading...", width, bodyHeight)
	case ctx.Err != nil:
		body = renderLogBrowserMessage(ctx.Err.Error(), width, bodyHeight)
	case ctx.Viewer:
		body = renderLogViewer(ctx.Lines, ctx.LineCursor, width, bodyHeight)
	default:
		body = renderLogBrowserItems(ctx.Items, ctx.Selected, ctx.Empty, width, bodyHeight)
	}
	footer := renderLogBrowserFooter(ctx, width)
	return strings.Join([]string{header, breadcrumb, body, footer}, "\n")
}

func renderLogBrowserHeader(ctx LogBrowserContext, width int) string {
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.Text).Render("pm2 logs")
	status := ""
	switch {
	case ctx.Notice != "":
		status = lipgloss.NewStyle().Foreground(theme.Warn).Render("  " + ctx.Notice)
	case ctx.Err != nil:
		status = lipgloss.NewStyle().Foreground(theme.Errored).Render("  error")
	}
	return lipgloss.NewStyle().
		Background(theme.HdrBg).
		Width(width).
		Padding(0, 1).
		Render(title + status)
}

func renderLogBrowserBreadcrumb(parts []string, width int) string {
	label := "applications"
	if len(parts) > 0 {
		label = strings.Join(parts, " → ")
	}
	return lipgloss.NewStyle().
		Foreground(theme.Muted).
		Width(width).
		Padding(0, 1).
		Render(CropRight(label, width-2))
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
		row := marker + CropRight(items[index], width-3)
		rows = append(rows, style.Width(width).Padding(0, 1).Render(row))
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
		Padding(0, 1).
		Render(CropRight(message, width-2))
	return padLogBrowserRows([]string{row}, width, height)
}

func renderLogBrowserFooter(ctx LogBrowserContext, width int) string {
	var hints []string
	switch {
	case ctx.ConfirmPath != "":
		hints = []string{"y confirm delete", "n/esc cancel"}
	case ctx.Viewer:
		hints = []string{"↑↓ / jk navigate", "d delete", "esc back", "q quit"}
	default:
		hints = []string{"↑↓ / jk navigate", "enter open"}
		if ctx.CanDelete {
			hints = append(hints, "d delete")
		}
		hints = append(hints, "esc back", "q quit")
	}
	return lipgloss.NewStyle().
		Background(theme.HdrBg).
		Foreground(theme.Muted).
		Width(width).
		Padding(0, 1).
		Render(CropRight(strings.Join(hints, "  │  "), width-2))
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
