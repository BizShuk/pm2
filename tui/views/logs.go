package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/bizshuk/pm2/tui/theme"
)

// RenderLogs renders the tail of the selected process's log inside the
// right panel. The logs slice is whatever the controller has stored —
// nil means "still loading", an empty slice means "no entries yet".
func RenderLogs(name string, logs []string, w, h int) string {
	hdr := secHeader(fmt.Sprintf("logs — %s", name), w)
	blank := strings.Repeat(" ", w)

	var rows []string
	if logs == nil {
		rows = append(rows, lipgloss.NewStyle().Width(w).Padding(0, 1).Foreground(theme.Muted).Render("loading..."))
	} else {
		for _, l := range logs {
			rows = append(rows, lipgloss.NewStyle().Width(w).Padding(0, 1).Foreground(theme.Muted).Render(Crop(l, w-3)))
		}
		if len(rows) == 0 {
			rows = append(rows, lipgloss.NewStyle().Width(w).Padding(0, 1).Foreground(theme.Muted).Render("(no log entries)"))
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