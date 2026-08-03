package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// hostMetricsMsg carries the latest host CPU/Memory readings back
// to Update. The sampling itself belongs to the sysmon package,
// which is the single owner of host measurement for both `pm2
// monitor` and `pm2 dashboard`; this file only owns the message
// types and the re-arm tick.
type hostMetricsMsg struct {
	cpu float64
	mem float64
}

// triggerHostMetricsMsg is fired by a tea.Tick to re-sample host
// metrics. The Update handler in model.go responds by calling
// m.hostMetrics.Sample() and emitting a hostMetricsMsg.
type triggerHostMetricsMsg struct{}

// hostMetricsFallbackCPU / hostMetricsFallbackMem are the
// cosmetic values rendered when the collector returns an error
// (sandboxed /proc, missing macOS iostat, etc). The numbers are
// deliberately stable: a footer that flickers between a real
// value and an error state is harder to read than one that holds
// still.
const (
	hostMetricsFallbackCPU = 5.2
	hostMetricsFallbackMem = 64.1
)

// updateHostMetricsCmd schedules a single host-metric sample after
// a short delay. Re-arming is done by Update: when it sees a
// hostMetricsMsg, it returns a new updateHostMetricsCmd that
// fires after the next interval.
//
// The delay is measured from the end of the previous sample, not
// from a fixed clock, because a sample blocks for about a second
// on macOS and a fixed ticker would queue them back to back.
func updateHostMetricsCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return triggerHostMetricsMsg{}
	})
}
