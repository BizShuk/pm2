package tui

import (
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/tui/hostmetrics"
	"github.com/bizshuk/pm2/tui/views"
)

const (
	refreshDur = 2 * time.Second
	maxLogTail = 14
	detailRows = 17 // rows in detail section (excluding header)
)

type SortField string

const (
	SortByName      SortField = "name"
	SortByNamespace SortField = "namespace"
	SortByCPU       SortField = "cpu"
	SortByMem       SortField = "memory"
	SortByStatus    SortField = "status"
)

// ─── messages ────────────────────────────────────────────────────────────────

type (
	tickMsg    time.Time
	refreshMsg struct {
		procs []process.ProcessInfo
		err   error
	}
)

type logsMsg struct {
	path  string
	lines []string
}

// actionMsg carries the result of a user action (restart/pause/resume/
// delete): the freshly refreshed process list plus an optional human
// notice describing an action failure to surface in the title bar.
type actionMsg struct {
	refreshMsg
	notice string
}

// ─── model ───────────────────────────────────────────────────────────────────

type Model struct {
	socket    string
	procs     []process.ProcessInfo
	allProcs  []process.ProcessInfo // unfiltered list — drives the namespace strip
	namespaces []string            // ["All"] + unique sorted namespaces from allProcs
	nsCursor   int                 // index into namespaces; 0 == All
	selected  int
	logs      []string
	width     int
	height    int
	err       error
	updated   time.Time
	Detail    bool
	logFocus  bool
	hostCPU   float64
	hostMem   float64
	// hostMetrics samples CPU/Mem via the platform-appropriate
	// collector. Held as an interface so tests can inject a stub.
	hostMetrics hostmetrics.HostMetricsCollector
	SortBy    SortField
	notice    string // transient message from the last action (e.g. a failure)
}

func New(socket string, detail bool) Model {
	return Model{
		socket:      socket,
		width:       120,
		height:      30,
		Detail:      detail,
		hostCPU:     hostMetricsFallbackCPU,
		hostMem:     hostMetricsFallbackMem,
		hostMetrics: hostmetrics.NewCollector(),
		SortBy:      SortByName,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		doRefresh(m.socket),
		tea.Tick(refreshDur, func(t time.Time) tea.Msg { return tickMsg(t) }),
		updateHostMetricsCmd(),
	)
}

// ─── update ──────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case tickMsg:
		m.notice = "" // clear a stale action notice on the next tick
		return m, tea.Batch(
			doRefresh(m.socket),
			tea.Tick(refreshDur, func(t time.Time) tea.Msg { return tickMsg(t) }),
		)

	case actionMsg:
		m.notice = msg.notice
		return m.applyRefresh(msg.refreshMsg)

	case refreshMsg:
		return m.applyRefresh(msg)

	case logsMsg:
		if len(m.procs) > 0 && m.selected >= 0 && m.selected < len(m.procs) {
			if m.procs[m.selected].LogFile == msg.path {
				if msg.lines == nil {
					m.logs = []string{}
				} else {
					m.logs = msg.lines
				}
			}
		}

	case hostMetricsMsg:
		m.hostCPU = msg.cpu
		m.hostMem = msg.mem
		return m, updateHostMetricsCmd()

	case triggerHostMetricsMsg:
		return m, func() tea.Msg {
			cpu, mem, err := m.hostMetrics.Collect()
			if err != nil {
				// Collector failed (sandboxed /proc, missing macOS
				// top, etc) — fall back to a stable cosmetic value
				// so the TUI never blanks out. The fallback constants
				// are defined in metrics.go next to the message types.
				cpu, mem = hostMetricsFallbackCPU, hostMetricsFallbackMem
			}
			return hostMetricsMsg{cpu: cpu, mem: mem}
		}

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// applyRefresh folds a refreshMsg into the model: it records any daemon
// error, replaces the process list while preserving the selected row,
// and (in detail mode) re-reads the selected process's log tail. Shared
// by the periodic refreshMsg and the post-action actionMsg.
func (m Model) applyRefresh(msg refreshMsg) (tea.Model, tea.Cmd) {
	m.err = msg.err
	if msg.err == nil {
		selectedID := -1
		if m.selected >= 0 && m.selected < len(m.procs) {
			selectedID = m.procs[m.selected].ID
		}
		m.allProcs = msg.procs
		m.recomputeNamespaces()
		m.applyNamespaceFilter()
		m.sortProcs(selectedID)
		m.updated = time.Now()
		if m.selected >= len(m.procs) {
			m.selected = max(0, len(m.procs)-1)
		}
		if len(m.procs) > 0 && m.Detail {
			return m, readLogs(m.procs[m.selected].LogFile)
		}
	}
	return m, nil
}

// recomputeNamespaces rebuilds the chip strip from the unfiltered
// process list. The "All" chip is always prepended (index 0). If the
// previously-selected namespace still exists after the rebuild, the
// cursor stays on it; otherwise the cursor falls back to "All".
//
// Empty namespaces (processes with AppConfig.Namespace unset) are
// normalised to "default" so the chip strip matches the daemon's
// Normalize behaviour and tests don't end up with a blank chip.
func (m *Model) recomputeNamespaces() {
	prev := ""
	if m.nsCursor >= 0 && m.nsCursor < len(m.namespaces) {
		prev = m.namespaces[m.nsCursor]
	}
	seen := make(map[string]struct{})
	for _, p := range m.allProcs {
		ns := p.Namespace
		if ns == "" {
			ns = "default"
		}
		seen[ns] = struct{}{}
	}
	rest := make([]string, 0, len(seen))
	for k := range seen {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	m.namespaces = append([]string{"All"}, rest...)

	if prev == "" {
		m.nsCursor = 0
		return
	}
	for i, n := range m.namespaces {
		if n == prev {
			m.nsCursor = i
			return
		}
	}
	m.nsCursor = 0
}

// applyNamespaceFilter rewrites m.procs as the subset of m.allProcs
// matching the currently-selected namespace chip. With nsCursor == 0
// ("All") the slice is a copy of the full list. The filtered list is
// sorted via sortProcs to preserve the controller's existing ordering
// guarantees, and m.selected is clamped so it stays in bounds.
func (m *Model) applyNamespaceFilter() {
	if len(m.namespaces) == 0 {
		m.procs = nil
		return
	}
	if m.nsCursor < 0 || m.nsCursor >= len(m.namespaces) {
		m.nsCursor = 0
	}
	if m.namespaces[m.nsCursor] == "All" {
		m.procs = make([]process.ProcessInfo, len(m.allProcs))
		copy(m.procs, m.allProcs)
		m.selected = max(0, min(m.selected, len(m.procs)-1))
		return
	}
	target := m.namespaces[m.nsCursor]
	filtered := m.procs[:0]
	for _, p := range m.allProcs {
		ns := p.Namespace
		if ns == "" {
			ns = "default"
		}
		if ns == target {
			filtered = append(filtered, p)
		}
	}
	m.procs = filtered
	m.selected = max(0, min(m.selected, len(m.procs)-1))
}

// cycleNamespace moves the namespace cursor by delta (positive =
// right, negative = left), wrapping at both ends, then re-applies the
// filter. The cursor is clamped defensively even though recomputeNamespaces
// keeps it in bounds under normal flow.
func (m *Model) cycleNamespace(delta int) {
	if len(m.namespaces) == 0 {
		return
	}
	n := len(m.namespaces)
	m.nsCursor = (m.nsCursor + delta) % n
	if m.nsCursor < 0 {
		m.nsCursor += n
	}
	m.applyNamespaceFilter()
	m.sortProcs()
	m.selected = max(0, min(m.selected, len(m.procs)-1))
}



func (m *Model) sortProcs(prevSelectedID ...int) {
	if len(m.procs) == 0 {
		return
	}
	selectedID := -1
	if len(prevSelectedID) > 0 {
		selectedID = prevSelectedID[0]
	}
	if selectedID == -1 && m.selected >= 0 && m.selected < len(m.procs) {
		selectedID = m.procs[m.selected].ID
	}

	sort.Slice(m.procs, func(i, j int) bool {
		pi, pj := m.procs[i], m.procs[j]
		switch m.SortBy {
		case SortByName:
			if pi.Name != pj.Name {
				return pi.Name < pj.Name
			}
			return pi.ID < pj.ID
		case SortByNamespace:
			if pi.Namespace != pj.Namespace {
				return pi.Namespace < pj.Namespace
			}
			if pi.Name != pj.Name {
				return pi.Name < pj.Name
			}
			return pi.ID < pj.ID
		case SortByCPU:
			if pi.CPU != pj.CPU {
				return pi.CPU > pj.CPU
			}
			return pi.Name < pj.Name
		case SortByMem:
			if pi.Memory != pj.Memory {
				return pi.Memory > pj.Memory
			}
			return pi.Name < pj.Name
		case SortByStatus:
			if pi.Status != pj.Status {
				return pi.Status < pj.Status
			}
			return pi.Name < pj.Name
		default:
			if pi.Name != pj.Name {
				return pi.Name < pj.Name
			}
			return pi.ID < pj.ID
		}
	})

	if selectedID != -1 {
		for idx, p := range m.procs {
			if p.ID == selectedID {
				m.selected = idx
				break
			}
		}
	}
}

func (m *Model) cycleSort() {
	switch m.SortBy {
	case SortByName:
		m.SortBy = SortByNamespace
	case SortByNamespace:
		m.SortBy = SortByCPU
	case SortByCPU:
		m.SortBy = SortByMem
	case SortByMem:
		m.SortBy = SortByStatus
	case SortByStatus:
		m.SortBy = SortByName
	default:
		m.SortBy = SortByName
	}
}

// the actual rendering to the tui/views package. No presentation
// logic lives here any more.
func (m Model) View() string {
	return views.RenderLayout(views.ViewContext{
		Width:      m.width,
		Height:     m.height,
		Selected:   m.selected,
		Procs:      m.procs,
		Namespaces: m.namespaces,
		NsCursor:   m.nsCursor,
		Logs:       m.logs,
		Updated:    m.updated,
		HostCPU:    m.hostCPU,
		HostMem:    m.hostMem,
		SortBy:     string(m.SortBy),
		Err:        m.err,
		Notice:     m.notice,
		Detail:     m.Detail,
		LogFocus:   m.logFocus,
	})
}

