package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
)

const (
	refreshDur = 2 * time.Second
	maxLogTail = 14
	detailRows = 17 // rows in detail section (excluding header)
)

// ─── colors ──────────────────────────────────────────────────────────────────

var (
	clOnline  = lipgloss.AdaptiveColor{Light: "#16a34a", Dark: "#3fb950"}
	clStopped = lipgloss.AdaptiveColor{Light: "#64748b", Dark: "#6e7681"}
	clErrored = lipgloss.AdaptiveColor{Light: "#dc2626", Dark: "#f85149"}
	clWarn    = lipgloss.AdaptiveColor{Light: "#d97706", Dark: "#e3b341"}
	clCron    = lipgloss.AdaptiveColor{Light: "#7c3aed", Dark: "#d2a8ff"}
	clPath    = lipgloss.AdaptiveColor{Light: "#1d4ed8", Dark: "#388bfd"}
	clMuted   = lipgloss.AdaptiveColor{Light: "#64748b", Dark: "#8b949e"}
	clSelBg   = lipgloss.AdaptiveColor{Light: "#e0e7ff", Dark: "#2e3440"}
	clSelName = lipgloss.AdaptiveColor{Light: "#0891b2", Dark: "#06b6d4"}
	clSelText = lipgloss.AdaptiveColor{Light: "#0f172a", Dark: "#ffffff"}
	clHdrBg   = lipgloss.AdaptiveColor{Light: "#f1f5f9", Dark: "#161b22"}
	clBorder  = lipgloss.AdaptiveColor{Light: "#cbd5e1", Dark: "#30363d"}
	clText    = lipgloss.AdaptiveColor{Light: "#0f172a", Dark: "#e6edf3"}
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

// ─── model ───────────────────────────────────────────────────────────────────

type Model struct {
	socket   string
	procs    []process.ProcessInfo
	selected int
	logs     []string
	width    int
	height   int
	err      error
	updated  time.Time
	Detail   bool
	hostCPU  float64
	hostMem  float64
	SortBy   SortField
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
		return m, tea.Batch(
			doRefresh(m.socket),
			tea.Tick(refreshDur, func(t time.Time) tea.Msg { return tickMsg(t) }),
		)

	case refreshMsg:
		m.err = msg.err
		if msg.err == nil {
			var selectedID int = -1
			if m.selected >= 0 && m.selected < len(m.procs) {
				selectedID = m.procs[m.selected].ID
			}
			m.procs = msg.procs
			m.sortProcs(selectedID)
			m.updated = time.Now()
			if m.selected >= len(m.procs) {
				m.selected = max(0, len(m.procs)-1)
			}
			if len(m.procs) > 0 && m.Detail {
				return m, readLogs(m.procs[m.selected].LogFile)
			}
		}

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
		return m, doAction(m.socket, model.Request{Command: model.CmdStop, Name: targetID})
	case "d":
		return m, doAction(m.socket, model.Request{Command: model.CmdDelete, Name: targetID})
	}
	return m, nil
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
func doAction(socket string, req model.Request) tea.Cmd {
	return func() tea.Msg {
		_, _ = model.SendRequest(socket, req)
		return doRefresh(socket)()
	}
}

// ─── view ────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width < 50 {
		return "terminal too narrow (min 50 cols)"
	}

	if !m.Detail {
		return m.buildListTUI()
	}

	contentH := m.height - 2 // subtract title + footer rows
	leftW := m.getLeftColW()
	rw := m.width - leftW - 1

	left := lipgloss.NewStyle().Width(leftW).Height(contentH).
		Render(m.buildLeft(leftW, contentH))

	div := lipgloss.NewStyle().Width(1).Height(contentH).Foreground(clBorder).
		Render(strings.Repeat("│\n", contentH-1) + "│")

	right := lipgloss.NewStyle().Width(rw).Height(contentH).
		Render(m.buildRight(rw, contentH))

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, div, right)
	return lipgloss.JoinVertical(lipgloss.Left, m.buildTitle(), body, buildFooter(m.width, m.SortBy))
}

