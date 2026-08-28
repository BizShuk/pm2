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

// DefaultInterval is the pause between the end of one collection and the
// start of the next, not a fixed period. A darwin sample already blocks
// for a second inside iostat, so re-arming on completion keeps the
// cadence honest instead of queueing ticks behind a slow sampler.
//
// It is deliberately long. Every sample re-ranks the list, so a fast
// cadence means rows sliding past a reader who is trying to read one of
// them; the cursor is anchored to its subject for the same reason. A
// shorter period is available through Interval when the question is
// "what is spiking right now" rather than "what is this machine doing".
const DefaultInterval = 30 * time.Second

// MinInterval floors the configurable period. A darwin collection blocks
// for about a second inside iostat, so anything below this queues the
// next pass behind the one still running.
const MinInterval = time.Second

// actionTTL is how long the result of a kill/stop stays on the footer.
// Long enough to read after a keystroke, short enough that it cannot be
// mistaken for a description of the current frame.
const actionTTL = 5 * time.Second

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
	// Interval is the pause between collections. Set it before handing
	// the model to Bubble Tea; zero means DefaultInterval.
	Interval time.Duration

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

	// anchorPID / anchorTask identify the highlighted subject rather than
	// its row number. Every collection re-ranks the list, so an index
	// alone would leave the cursor — and the detail pane under it — on
	// whichever process happened to inherit that position.
	anchorPID  int
	anchorTask string

	confirm  *killTarget // pending `d` confirmation, nil when none
	action   string      // result of the last kill/stop
	actionAt time.Time
}

// New returns a dashboard bound to the daemon socket. The daemon only
// supplies the managed-task list: with no daemon running the machine
// panel and the process list still work, which is the point of a system
// monitor.
func New(socket string) Model {
	return Model{
		Interval:  DefaultInterval,
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
		m.expireAction()
		m.applySort()
		m.restoreSelection()
		return m, tea.Tick(m.refreshDelay(), func(t time.Time) tea.Msg { return tickMsg(t) })

	case killResultMsg:
		m.action, m.actionAt = message.notice, time.Now()
		return m, nil

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

// refreshDelay clamps the configured period to something a collection
// can actually keep up with.
func (m Model) refreshDelay() time.Duration {
	if m.Interval < MinInterval {
		return DefaultInterval
	}
	return m.Interval
}

// rememberSelection records what the cursor is pointing at, so the next
// re-ranking can put it back on the same subject.
func (m *Model) rememberSelection() {
	m.anchorPID, m.anchorTask = 0, ""
	if m.scope == ScopeSystem {
		if m.selected >= 0 && m.selected < len(m.ranked) {
			m.anchorPID = m.ranked[m.selected].PID
		}
		return
	}
	tasks := m.observation.Snapshot.Tasks
	if m.selected >= 0 && m.selected < len(tasks) {
		m.anchorTask = tasks[m.selected].Name
	}
}

// restoreSelection re-finds the anchored subject after a re-rank. A
// subject that has gone away falls back to the clamped row index: the
// cursor stays where it is on screen rather than jumping to the top,
// which is what a reader expects when a neighbouring process exits.
func (m *Model) restoreSelection() {
	switch {
	case m.scope == ScopeSystem && m.anchorPID != 0:
		for i, proc := range m.ranked {
			if proc.PID == m.anchorPID {
				m.selected = i
				return
			}
		}
	case m.scope == ScopeTasks && m.anchorTask != "":
		for i, task := range m.observation.Snapshot.Tasks {
			if task.Name == m.anchorTask {
				m.selected = i
				return
			}
		}
	}
	m.clampSelection()
}

// rowCount is the number of selectable rows in the active scope.
func (m Model) rowCount() int {
	if m.scope == ScopeSystem {
		return len(m.ranked)
	}
	return len(m.observation.Snapshot.Tasks)
}

// expireAction drops a kill result once it is old enough that the list
// beneath it has already caught up with what happened.
func (m *Model) expireAction() {
	if m.action != "" && time.Since(m.actionAt) > actionTTL {
		m.action, m.actionAt = "", time.Time{}
	}
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
		Action:   m.action,
	}
	if m.confirm != nil {
		ctx.Confirm = m.confirm.prompt()
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
