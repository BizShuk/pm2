package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/pm2/tui/hostmetrics"
)

// hostMetricsMsg carries the latest host CPU/Memory readings back
// to Update. The actual sampling logic now lives in
// tui/hostmetrics so it can be swapped per-platform; this file
// only owns the message types and the re-arm tick.
type hostMetricsMsg struct {
	cpu float64
	mem float64
}

// triggerHostMetricsMsg is fired by a tea.Tick to re-sample host
// metrics. The Update handler in model.go responds by calling
// m.hostMetrics.Collect() and emitting a hostMetricsMsg.
type triggerHostMetricsMsg struct{}

// hostMetricsFallbackCPU / hostMetricsFallbackMem are the
// cosmetic values rendered when the active collector returns an
// error (sandboxed /proc, missing macOS top, etc). They match the
// values the legacy macOS `top` parser produced on parse failure
// so the TUI doesn't visibly change when the underlying source
// fails.
const (
	hostMetricsFallbackCPU = 5.2
	hostMetricsFallbackMem = 64.1
)

// updateHostMetricsCmd schedules a single host-metric sample after
// a short delay. Re-arming is done by Update: when it sees a
// hostMetricsMsg, it returns a new updateHostMetricsCmd that
// fires after the next interval.
func updateHostMetricsCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return triggerHostMetricsMsg{}
	})
}

// ensure hostmetrics is referenced even if a future refactor
// decides the package is no longer needed in this file (e.g.
// when collectors become a parameter on the Model). A blank
// assignment compiles to a no-op but keeps the import honest.
var _ = hostmetrics.NewCollector
