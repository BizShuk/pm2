package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
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
	SortBy    SortField
	notice    string // transient message from the last action (e.g. a failure)
}

func New(socket string, detail bool) Model {
	return Model{
		socket:  socket,
		width:   120,
		height:  30,
		Detail:  detail,
		hostCPU: 5.2,
		hostMem: 64.1,
		SortBy:  SortByName,
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
			cpu, mem := collectHostMetrics()
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

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	if msg.String() == "s" {
		m.cycleSort()
		m.sortProcs()
		return m, nil
	}
	// Namespace switching is intentionally available even when the
	// current filter has zero rows — otherwise a user could get stuck
	// on an empty namespace with no way back.
	switch msg.String() {
	case "left":
		m.cycleNamespace(-1)
		return m, nil
	case "right":
		m.cycleNamespace(+1)
		return m, nil
	}
	if len(m.procs) == 0 {
		return m, nil
	}
	targetID := fmt.Sprintf("%d", m.procs[m.selected].ID)
	switch msg.String() {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
			if m.Detail {
				m.logs = nil
				return m, readLogs(m.procs[m.selected].LogFile)
			}
			return m, nil
		}
	case "down", "j":
		if m.selected < len(m.procs)-1 {
			m.selected++
			if m.Detail {
				m.logs = nil
				return m, readLogs(m.procs[m.selected].LogFile)
			}
			return m, nil
		}
	case "r":
		return m, doAction(m.socket, model.Request{Command: model.CmdRestart, Name: targetID})
	case "p":
		// Toggle pause/resume on the selected process. Pausing a cron
		// task suspends its schedule (status → paused) and stops any
		// running instance; pressing again resumes it (a cron task
		// returns to idle, a regular process comes back online). The
		// selected status was set by the last refresh, and doAction
		// refreshes immediately, so successive presses flip cleanly.
		cmd := pauseOrResume(m.procs[m.selected].Status)
		return m, doAction(m.socket, model.Request{Command: cmd, Name: targetID})
	case "d":
		return m, doAction(m.socket, model.Request{Command: model.CmdDelete, Name: targetID})
	case "enter":
		// Toggle log-focus: in two-pane mode, hide the detail block and
		// show the log tail filling the full right pane. Pressing Enter
		// again restores the detail+logs view. No-op in wide-table mode
		// (where there's no detail block to hide) and on an empty list.
		if m.Detail {
			m.logFocus = !m.logFocus
		}
		return m, nil
	case "esc":
		// Convenience exit from log-focus. No-op in wide-table mode and
		// when log-focus is already off, so it never steals Esc from
		// any other future binding.
		if m.Detail {
			m.logFocus = false
		}
		return m, nil
	}
	return m, nil
}

// pauseOrResume picks the RPC command for the `p` key toggle: a paused
// process resumes, anything else pauses.
func pauseOrResume(s process.Status) model.CommandType {
	if s == process.StatusPaused {
		return model.CmdResume
	}
	return model.CmdPause
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

// ─── commands ────────────────────────────────────────────────────────────────

func doRefresh(socket string) tea.Cmd {
	return func() tea.Msg {
		resp, err := model.SendRequest(socket, model.Request{Command: model.CmdList})
		if err != nil {
			return refreshMsg{err: err}
		}
		var procs []process.ProcessInfo
		if err := json.Unmarshal(resp.Payload, &procs); err != nil {
			return refreshMsg{err: err}
		}
		sort.Slice(procs, func(i, j int) bool { return procs[i].ID < procs[j].ID })
		return refreshMsg{procs: procs}
	}
}

func readLogs(path string) tea.Cmd {
	return func() tea.Msg {
		f, err := os.Open(path)
		if err != nil {
			return logsMsg{path: path}
		}
		defer f.Close()
		var lines []string
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
		if err := sc.Err(); err != nil {
			// Ignore or log error
		}
		if len(lines) > maxLogTail {
			lines = lines[len(lines)-maxLogTail:]
		}
		return logsMsg{path: path, lines: lines}
	}
}

// doAction sends an RPC then immediately re-fetches the process list.
// The action's outcome is threaded back so the UI can report a failure
// instead of silently swallowing it — e.g. a stale daemon that does not
// recognise `pause`/`resume` replies "unknown command", which would
// otherwise leave the status looking unchanged with no explanation.
func doAction(socket string, req model.Request) tea.Cmd {
	return func() tea.Msg {
		var notice string
		resp, err := model.SendRequest(socket, req)
		switch {
		case err != nil:
			notice = fmt.Sprintf("%s failed: %v", req.Command, err)
		case resp != nil && !resp.OK:
			notice = fmt.Sprintf("%s failed: %s", req.Command, resp.Error)
		}
		refresh := doRefresh(socket)().(refreshMsg)
		return actionMsg{refreshMsg: refresh, notice: notice}
	}
}

// ─── view ────────────────────────────────────────────────────────────────────

// View builds a ViewContext from the controller state and delegates
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

