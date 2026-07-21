package views

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/bizshuk/pm2/tui/theme"
)

// RenderHeader renders the top title bar with process count / error
// indicator / transient action notice. Pure function — see ViewContext.
func RenderHeader(ctx ViewContext) string {
	name := lipgloss.NewStyle().Bold(true).Foreground(theme.Text).Render("pm2 monitor")
	var info string
	switch {
	case ctx.Notice != "":
		info = lipgloss.NewStyle().Foreground(theme.Errored).Render("  ✗ " + ctx.Notice)
	case ctx.Err != nil:
		info = lipgloss.NewStyle().Foreground(theme.Errored).Render("  ✗ daemon unreachable")
	case !ctx.Updated.IsZero():
		info = lipgloss.NewStyle().Foreground(theme.Muted).Render(
			fmt.Sprintf("  %d processes · %s", len(ctx.Procs), ctx.Updated.Format("15:04:05")),
		)
	}
	return lipgloss.NewStyle().Background(theme.HdrBg).Width(ctx.Width).Padding(0, 1).Render(name + info)
}
