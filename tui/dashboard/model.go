// Package dashboard is the controller for `pm2 dashboard`, the activity
// monitor: whole-machine resource usage on top, a selectable list of pm2
// tasks or OS processes below, and a detail pane breaking the selection
// down into its sub-processes and listening ports.
//
// It owns interaction state only. Measurement belongs to sysmon and
// rendering to tui/views, and neither is allowed to leak in here.
package dashboard

import (
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/pm2/sysmon"
	"github.com/bizshuk/pm2/tui/views"
)

// refreshDelay is the pause between the end of one collection and the
// start of the next, not a fixed period. A darwin sample already blocks
// for a second inside iostat, so re-arming on completion keeps the
// cadence honest instead of queueing ticks behind a slow sampler.
const refreshDelay = time.Second

// Scope selects what the list pane enumerates.
type Scope string

const (
	// ScopeTasks lists the applications pm2 manages.
	ScopeTasks Scope = views.ScopeTasks
	// ScopeSystem lists every process on the machine.
	ScopeSystem Scope = views.ScopeSystem
)

// SortField orders the list pane.
type SortField string

const (
	SortByCPU    SortField = "cpu"
	SortByMemory SortField = "memory"
	SortByName   SortField = "name"
)

// Model is the Bubble Tea model behind `pm2 dashboard`.
type Model struct {
	socket    string
	collector *sysmon.Collector

	observation sysmon.Observation
	ranked      []sysmon.Proc // process table in the active sort order

	scope    Scope
	sortBy   SortField
	selected int
	width    int
	height   int
	updated  time.Time
	notice   string
}

// New returns a dashboard bound to the daemon socket. The daemon only
// supplies the managed-task list: with no daemon running the machine
// panel and the process list still work, which is the point of a system
// monitor.
func New(socket string) Model {
	return Model{
		socket:    socket,
		collector: sysmon.New(),
		scope:     ScopeTasks,
		sortBy:    SortByCPU,
		width:     120,
		height:    34,
	}
}

func (m Model) Init() tea.Cmd {
	return collect(m.socket, m.collector)
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height

	case observationMsg:
		m.observation = message.observation
		m.updated = time.Now()
		m.notice = message.notice
		m.applySort()
		m.clampSelection()
		return m, tea.Tick(refreshDelay, func(t time.Time) tea.Msg { return tickMsg(t) })

	case tickMsg:
		return m, collect(m.socket, m.collector)

	case tea.KeyMsg:
		return m.handleKey(message)
	}
	return m, nil
}

// applySort orders both list scopes so toggling scope keeps the same
// ranking rule. Ties fall back to name, then PID, so rows do not swap
// places between samples that report identical usage.
func (m *Model) applySort() {
	tasks := m.observation.Snapshot.Tasks
	sort.SliceStable(tasks, func(i, j int) bool {
		switch m.sortBy {
		case SortByMemory:
			if tasks[i].TreeMemoryBytes != tasks[j].TreeMemoryBytes {
				return tasks[i].TreeMemoryBytes > tasks[j].TreeMemoryBytes
			}
		case SortByName:
		default:
			if tasks[i].TreeCPUPercent != tasks[j].TreeCPUPercent {
				return tasks[i].TreeCPUPercent > tasks[j].TreeCPUPercent
			}
		}
		return tasks[i].Name < tasks[j].Name
	})

	ranked := append([]sysmon.Proc(nil), m.observation.Procs...)
	sort.SliceStable(ranked, func(i, j int) bool {
		switch m.sortBy {
		case SortByMemory:
			if ranked[i].MemoryBytes != ranked[j].MemoryBytes {
				return ranked[i].MemoryBytes > ranked[j].MemoryBytes
			}
		case SortByName:
			if ranked[i].Executable() != ranked[j].Executable() {
				return ranked[i].Executable() < ranked[j].Executable()
			}
		default:
			if ranked[i].CPUPercent != ranked[j].CPUPercent {
				return ranked[i].CPUPercent > ranked[j].CPUPercent
			}
		}
		return ranked[i].PID < ranked[j].PID
	})
	m.ranked = ranked
}

// rowCount is the number of selectable rows in the active scope.
func (m Model) rowCount() int {
	if m.scope == ScopeSystem {
		return len(m.ranked)
	}
	return len(m.observation.Snapshot.Tasks)
}

func (m *Model) clampSelection() {
	if m.selected >= m.rowCount() {
		m.selected = max(0, m.rowCount()-1)
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

// View builds the render context for the current frame. Resolving the
// selected subject's children and ports happens here rather than in a
// renderer because only the controller holds the process table the walk
// needs — and doing it per frame avoids a cache that could disagree with
// the list the user is looking at.
func (m Model) View() string {
	ctx := views.DashboardContext{
		Width:    m.width,
		Height:   m.height,
		Snapshot: m.observation.Snapshot,
		Procs:    m.ranked,
		Selected: m.selected,
		Scope:    string(m.scope),
		SortBy:   string(m.sortBy),
		Updated:  m.updated,
		Notice:   m.notice,
	}

	if m.scope == ScopeSystem {
		if m.selected >= 0 && m.selected < len(m.ranked) {
			proc := m.ranked[m.selected]
			ctx.Proc = &proc
			ctx.Children = sysmon.Descendants(m.observation.Procs, proc.PID)
			ctx.Ports = sysmon.PortsFor(m.observation.Ports, proc.PID, ctx.Children)
		}
		return views.RenderDashboard(ctx)
	}

	tasks := m.observation.Snapshot.Tasks
	if m.selected >= 0 && m.selected < len(tasks) {
		task := tasks[m.selected]
		ctx.Task = &task
		ctx.Children = task.Children
		ctx.Ports = task.Ports
	}
	return views.RenderDashboard(ctx)
}
