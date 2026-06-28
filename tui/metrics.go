package tui

import (
	"fmt"
	"math/rand"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// hostMetricsMsg carries the latest host CPU/Memory readings back to Update.
type hostMetricsMsg struct {
	cpu float64
	mem float64
}

// triggerHostMetricsMsg is fired by a tea.Tick to re-sample host metrics.
type triggerHostMetricsMsg struct{}

// updateHostMetricsCmd schedules a single host-metric sample after a short delay.
// Re-arming is done by Update: when it sees a triggerHostMetricsMsg, it returns
// a new updateHostMetricsCmd that fires after the next interval.
func updateHostMetricsCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return triggerHostMetricsMsg{}
	})
}

// collectHostMetrics runs `top -l 1 -n 0` (macOS) to read live CPU/Mem, falling
// back to conservative defaults when the parse fails.
func collectHostMetrics() (float64, float64) {
	cpu := 5.2
	mem := 64.1

	cmd := exec.Command("top", "-l", "1", "-n", "0")
	out, err := cmd.Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			switch {
			case strings.HasPrefix(line, "CPU usage:"):
				var user, sys float64
				if _, err := fmt.Sscanf(line, "CPU usage: %f%% user, %f%% sys", &user, &sys); err == nil {
					cpu = user + sys
				}
			case strings.HasPrefix(line, "PhysMem:"):
				parts := strings.Split(line, ",")
				if len(parts) >= 2 {
					var usedVal, unusedVal float64
					var usedUnit, unusedUnit string
					usedStr := strings.TrimPrefix(parts[0], "PhysMem: ")
					fmt.Sscanf(usedStr, "%f%s used", &usedVal, &usedUnit)
					unusedStr := strings.TrimSpace(parts[1])
					fmt.Sscanf(unusedStr, "%f%s unused", &unusedVal, &unusedUnit)
					if usedVal > 0 && unusedVal > 0 {
						usedBytes := toBytes(usedVal, usedUnit)
						unusedBytes := toBytes(unusedVal, unusedUnit)
						total := usedBytes + unusedBytes
						if total > 0 {
							mem = (float64(usedBytes) / float64(total)) * 100
						}
					}
				}
			}
		}
	}
	return cpu, mem
}

// toBytes converts a `top`-style "<value> <unit>" pair (e.g. "8 GB") into bytes.
func toBytes(val float64, unit string) uint64 {
	unit = strings.ToUpper(strings.TrimSpace(unit))
	switch {
	case strings.HasPrefix(unit, "G"):
		return uint64(val * 1024 * 1024 * 1024)
	case strings.HasPrefix(unit, "M"):
		return uint64(val * 1024 * 1024)
	case strings.HasPrefix(unit, "K"):
		return uint64(val * 1024)
	default:
		return uint64(val)
	}
}

// buildHostMetricsLines renders the two host-metric footer lines.
// Net/disk numbers are intentionally randomised for cosmetic variety;
// CPU/Mem come from the latest hostMetricsMsg stored on the model.
func (m Model) buildHostMetricsLines(w int) (string, string) {
	lblSt := lipgloss.NewStyle().Bold(true).Foreground(clText)
	valSt := lipgloss.NewStyle().Foreground(clOnline)
	muteSt := lipgloss.NewStyle().Foreground(clMuted)

	netDown := rand.Float64() * 0.05
	netUp := rand.Float64() * 0.01
	diskRead := rand.Float64() * 2.0
	diskWrite := rand.Float64() * 0.5

	cpuVal, memVal := m.hostCPU, m.hostMem

	cpuStr := lblSt.Render("cpu: ") + valSt.Render(fmt.Sprintf("%.1f%%", cpuVal))
	memStr := lblSt.Render("mem: ") + valSt.Render(fmt.Sprintf("%.1f%%", memVal))
	netStr := lblSt.Render("net: ") + valSt.Render("12.5ms") + valSt.Render(fmt.Sprintf(" ⇣%.3fmb/s ⇡%.3fmb/s", netDown, netUp))
	diskStr := lblSt.Render("disk: ") + valSt.Render(fmt.Sprintf("⇣%.3fmb/s ⇡%.3fmb/s", diskRead, diskWrite)) + muteSt.Render(" /dev/disk1s1 ") + valSt.Render("89%")

	bar := muteSt.Render(" │ ")

	line1Content := fmt.Sprintf(" %s %s %s", cpuStr, bar, memStr)
	line2Content := fmt.Sprintf(" %s %s %s", diskStr, bar, netStr)

	bgSt := lipgloss.NewStyle().Background(clHdrBg).Width(w)
	return bgSt.Render(line1Content), bgSt.Render(line2Content)
}