package dashboard

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/sysmon"
)

type tickMsg time.Time

// observationMsg carries one finished collection back to Update. notice
// holds a human explanation when part of the collection failed; the
// observation itself is still delivered, so an unreachable daemon costs
// the task list and nothing else.
type observationMsg struct {
	observation sysmon.Observation
	notice      string
}

// collect samples the machine and the daemon in one background pass.
//
// It is deliberately a single command rather than one per source: the
// panel, the list and the detail pane all describe the same instant, and
// refreshing them on separate tickers would show a task's CPU from one
// second next to the machine's CPU from another.
func collect(socket string, collector *sysmon.Collector) tea.Cmd {
	return func() tea.Msg {
		// model.ListProcesses is used instead of the cmd/runtime client so
		// tui/ never depends on cmd/ — and so opening a dashboard never
		// silently spawns a daemon the user chose to stop.
		managed, err := model.ListProcesses(socket)

		message := observationMsg{observation: collector.Observe(managed)}
		if err != nil {
			message.notice = "daemon unreachable — showing system only"
		} else if failures := message.observation.Snapshot.Errors; len(failures) > 0 {
			message.notice = failures[0]
		}
		return message
	}
}
