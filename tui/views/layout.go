package views

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/bizshuk/pm2/tui/theme"
)

// RenderLayout is the single entry point the controller calls per
// frame. It decides between the wide-table list and the two-pane
// detail view, computes the left-column width when in two-pane mode,
// and stitches the header / body / footer together.
func RenderLayout(ctx ViewContext) string {
	if ctx.Width < 50 {
		return "terminal too narrow (min 50 cols)"
	}

	if !ctx.Detail {
		return RenderWideTable(ctx)
	}

	contentH := ctx.Height - 3 // subtract title + namespace bar + footer rows
	leftW := leftColumnWidth(ctx)
	rw := ctx.Width - leftW - 1

	left := lipgloss.NewStyle().Width(leftW).Height(contentH).
		Render(RenderLeftPane(ctx, leftW, contentH))

	div := lipgloss.NewStyle().Width(1).Height(contentH).Foreground(theme.Border).
		Render(strings.Repeat("│\n", contentH-1) + "│")

	right := lipgloss.NewStyle().Width(rw).Height(contentH).
		Render(RenderRightPane(ctx, rw, contentH))

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, div, right)
	return lipgloss.JoinVertical(lipgloss.Left,
		RenderHeader(ctx),
		RenderNamespaceBar(ctx, ctx.Width),
		body,
		RenderFooter(ctx.Width, ctx.SortBy))
}

// RenderRightPane renders the right panel: detail + logs (when height
// allows). Pure function — see ViewContext.
func RenderRightPane(ctx ViewContext, w, h int) string {
	if len(ctx.Procs) == 0 {
		return lipgloss.NewStyle().Width(w).Padding(1, 2).Foreground(theme.Muted).
			Render("no processes\nstart one: pm2 start <script>")
	}
	p := ctx.Procs[ctx.Selected]
	detail := RenderDetail(p, w)
	if h < 20 {
		return detail
	}
	logH := h - detailRows - 3 // detailRows (17) + detail header (1) + divider newline (1) + log header (1) = 20
	logs := RenderLogs(p.Name, ctx.Logs, w, logH)
	return detail + "\n" + logs
}

// leftColumnWidth picks the left-panel column width so that process
// rows are comfortably padded while leaving at least 40 columns for
// the right panel.
func leftColumnWidth(ctx ViewContext) int {
	w := 34

	if len(ctx.Procs) > 0 {
		maxNameLen := 0
		maxUpLen := 0
		for _, p := range ctx.Procs {
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
	maxAllowedW := ctx.Width - minRightW - 1 // 1 for the divider
	if maxAllowedW < 34 {
		maxAllowedW = 34
	}
	if maxAllowedW > 50 {
		maxAllowedW = 50
	}
	if w > maxAllowedW {
		w = maxAllowedW
	}
	if w > ctx.Width-10 {
		w = ctx.Width - 10
	}
	if w < 10 {
		w = 10
	}

	return w
}