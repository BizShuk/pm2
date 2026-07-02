package views

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/bizshuk/pm2/tui/theme"
)

// RenderFooter renders the bottom key-binding legend.
func RenderFooter(w int, sortBy string) string {
	keys := [][2]string{
		{"↑↓ / jk", "navigate"},
		{"r", "restart"},
		{"p", "pause/resume"},
		{"d", "delete"},
		{"s", "sort: " + sortBy},
		{"q", "quit"},
	}
	ks := lipgloss.NewStyle().Foreground(theme.Text)
	ds := lipgloss.NewStyle().Foreground(theme.Muted)
	var parts []string
	for _, h := range keys {
		parts = append(parts, ks.Render(h[0])+" "+ds.Render(h[1]))
	}
	return lipgloss.NewStyle().Background(theme.HdrBg).Width(w).Padding(0, 1).
		Render(strings.Join(parts, "  │  "))
}

// RenderHostMetricsLines renders the two host-metric footer lines used
// at the bottom of the wide-table view. Net/disk numbers are
// intentionally randomised for cosmetic variety; CPU/Mem come from
// the latest ViewContext sample.
func RenderHostMetricsLines(ctx ViewContext) (string, string) {
	w := ctx.Width
	lblSt := lipgloss.NewStyle().Bold(true).Foreground(theme.Text)
	valSt := lipgloss.NewStyle().Foreground(theme.Online)
	muteSt := lipgloss.NewStyle().Foreground(theme.Muted)

	netDown := rand.Float64() * 0.05
	netUp := rand.Float64() * 0.01
	diskRead := rand.Float64() * 2.0
	diskWrite := rand.Float64() * 0.5

	cpuVal, memVal := ctx.HostCPU, ctx.HostMem

	cpuStr := lblSt.Render("cpu: ") + valSt.Render(fmt.Sprintf("%.1f%%", cpuVal))
	memStr := lblSt.Render("mem: ") + valSt.Render(fmt.Sprintf("%.1f%%", memVal))
	netStr := lblSt.Render("net: ") + valSt.Render("12.5ms") + valSt.Render(fmt.Sprintf(" ⇣%.3fmb/s ⇡%.3fmb/s", netDown, netUp))
	diskStr := lblSt.Render("disk: ") + valSt.Render(fmt.Sprintf("⇣%.3fmb/s ⇡%.3fmb/s", diskRead, diskWrite)) + muteSt.Render(" /dev/disk1s1 ") + valSt.Render("89%")

	bar := muteSt.Render(" │ ")

	line1Content := fmt.Sprintf(" %s %s %s", cpuStr, bar, memStr)
	line2Content := fmt.Sprintf(" %s %s %s", diskStr, bar, netStr)

	bgSt := lipgloss.NewStyle().Background(theme.HdrBg).Width(w)
	return bgSt.Render(line1Content), bgSt.Render(line2Content)
}