package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/bizshuk/pm2/sysmon"
	"github.com/bizshuk/pm2/tui/theme"
)

// Gauge fill thresholds. Below warn a bar is green, between warn and
// critical amber, above critical red — the same traffic-light reading a
// user already has for process status.
const (
	gaugeWarnPercent     = 70.0
	gaugeCriticalPercent = 88.0
	gaugeWidth           = 18
	hostLabelWidth       = 6
)

// RenderHostPanel draws the fixed whole-machine block: CPU, memory,
// network and disk, one line each. Pure function of a sysmon.System.
func RenderHostPanel(system sysmon.System, width int) string {
	lines := []string{
		hostLine("cpu", gauge(system.CPU.UsedPercent, gaugeWidth), cpuSummary(system)),
		hostLine("mem", gauge(system.Memory.UsedPercent, gaugeWidth), memorySummary(system.Memory)),
		hostLine("net", "", networkSummary(system.Network)),
		hostLine("disk", "", diskSummary(system)),
	}

	// Trimming is left to lipgloss's MaxWidth, which measures printable
	// columns. Crop/CropRight count raw bytes, so handing them a styled
	// string makes every colour escape look like visible text and the
	// line loses its tail long before it reaches the edge.
	background := lipgloss.NewStyle().Background(theme.HdrBg).
		Width(width).MaxWidth(width).MaxHeight(1)
	for index, line := range lines {
		lines[index] = background.Render(" " + line)
	}
	return strings.Join(lines, "\n")
}

func hostLine(label, bar, summary string) string {
	labelText := lipgloss.NewStyle().Bold(true).Foreground(theme.Text).
		Width(hostLabelWidth).Render(label)
	if bar == "" {
		return labelText + strings.Repeat(" ", gaugeWidth+1) + summary
	}
	return labelText + bar + " " + summary
}

// gauge renders a proportional bar. A reading the platform could not
// produce arrives as zero, which draws an empty bar rather than a
// misleading full one.
func gauge(percent float64, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int(percent/100*float64(width) + 0.5)

	colour := theme.Online
	switch {
	case percent >= gaugeCriticalPercent:
		colour = theme.Errored
	case percent >= gaugeWarnPercent:
		colour = theme.Warn
	}

	return lipgloss.NewStyle().Foreground(colour).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(theme.Border).Render(strings.Repeat("░", width-filled))
}

func cpuSummary(system sysmon.System) string {
	value := lipgloss.NewStyle().Bold(true).Foreground(theme.Text).
		Render(fmt.Sprintf("%5.1f%%", system.CPU.UsedPercent))
	detail := fmt.Sprintf("user %.1f  sys %.1f  ·  load %.2f %.2f %.2f  ·  %d cores",
		system.CPU.UserPercent,
		system.CPU.SysPercent,
		system.Load.One,
		system.Load.Five,
		system.Load.Fifteen,
		system.CPU.Cores,
	)
	return value + "  " + mutedText(detail)
}

// memorySummary leads with the platform's own "used" percentage but
// always shows available bytes beside it: on macOS the percentage sits
// near 99 by design, and without the headroom figure the panel would look
// like a machine permanently out of memory.
func memorySummary(memory sysmon.Memory) string {
	value := lipgloss.NewStyle().Bold(true).Foreground(theme.Text).
		Render(fmt.Sprintf("%5.1f%%", memory.UsedPercent))
	detail := fmt.Sprintf("%s of %s  ·  %s available",
		formatBytes(memory.UsedBytes),
		formatBytes(memory.TotalBytes),
		formatBytes(memory.AvailableBytes),
	)
	if memory.SwapTotalBytes > 0 {
		detail += fmt.Sprintf("  ·  swap %s / %s",
			formatBytes(memory.SwapUsedBytes),
			formatBytes(memory.SwapTotalBytes),
		)
	}
	return value + "  " + mutedText(detail)
}

func networkSummary(network sysmon.Network) string {
	name := network.Interface
	if name == "" {
		name = "—"
	}
	value := lipgloss.NewStyle().Foreground(theme.Online).Render(fmt.Sprintf("⇣ %s   ⇡ %s",
		formatRate(network.RxBytesPerSecond),
		formatRate(network.TxBytesPerSecond),
	))
	return value + "  " + mutedText(fmt.Sprintf("on %s  ·  %s in / %s out since boot",
		name,
		formatBytes(network.RxBytesTotal),
		formatBytes(network.TxBytesTotal),
	))
}

// diskSummary pairs throughput with capacity. Read and write are split
// only where the platform reports them separately; macOS reports one
// combined figure, so it renders as "⇅".
func diskSummary(system sysmon.System) string {
	io := system.DiskIO
	throughput := fmt.Sprintf("⇅ %s", formatRate(io.BytesPerSecond))
	if io.ReadWriteSplit {
		throughput = fmt.Sprintf("⇣ %s   ⇡ %s",
			formatRate(io.ReadBytesPerSecond),
			formatRate(io.WriteBytesPerSecond),
		)
	}

	value := lipgloss.NewStyle().Foreground(theme.Online).Render(throughput)
	parts := []string{fmt.Sprintf("%.0f io/s", io.TransfersPerSecond)}
	for _, disk := range system.Disks {
		if len(parts) > 2 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s %s/%s %.0f%%",
			disk.Mount,
			formatBytes(disk.UsedBytes),
			formatBytes(disk.TotalBytes),
			disk.UsedPercent,
		))
	}
	return value + "  " + mutedText(strings.Join(parts, "  ·  "))
}

func mutedText(text string) string {
	return lipgloss.NewStyle().Foreground(theme.Muted).Render(text)
}
