package views

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/bizshuk/pm2/tui/theme"
)

// renderWrappedDetailField draws a label beside a value that may span
// multiple terminal rows. Continuation rows stay aligned with the value.
func renderWrappedDetailField(label, value string, width, keyWidth int, colour lipgloss.TerminalColor) string {
	contentWidth := max(width-2, 1)
	keyWidth = min(keyWidth, max(contentWidth-2, 1))
	valueWidth := max(contentWidth-keyWidth-1, 1)

	key := lipgloss.NewStyle().Foreground(theme.Muted).Width(keyWidth).Render(label)
	wrappedValue := lipgloss.NewStyle().Foreground(colour).Width(valueWidth).Render(value)
	content := lipgloss.JoinHorizontal(lipgloss.Top, key+" ", wrappedValue)

	return lipgloss.NewStyle().Width(width).Padding(0, 1).Render(content)
}
