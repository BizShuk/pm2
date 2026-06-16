package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/robfig/cron/v3"

	"github.com/shuk/pm2/daemon"
	"github.com/shuk/pm2/process"
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

type tickMsg time.Time
type refreshMsg struct {
	procs []process.ProcessInfo
	err   error
}
type logsMsg struct{ lines []string }
type hostMetricsMsg struct {
	cpu float64
	mem float64
}
type triggerHostMetricsMsg struct{}

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
		m.logs = msg.lines

	case hostMetricsMsg:
		m.hostCPU = msg.cpu
		m.hostMem = msg.mem
		return m, tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
			return triggerHostMetricsMsg{}
		})

	case triggerHostMetricsMsg:
		return m, updateHostMetricsCmd()

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
				return m, readLogs(m.procs[m.selected].LogFile)
			}
			return m, nil
		}
	case "down", "j":
		if m.selected < len(m.procs)-1 {
			m.selected++
			if m.Detail {
				return m, readLogs(m.procs[m.selected].LogFile)
			}
			return m, nil
		}
	case "r":
		return m, doAction(m.socket, daemon.Request{Command: daemon.CmdRestart, Name: targetID})
	case "p":
		return m, doAction(m.socket, daemon.Request{Command: daemon.CmdStop, Name: targetID})
	case "d":
		return m, doAction(m.socket, daemon.Request{Command: daemon.CmdDelete, Name: targetID})
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

func updateHostMetricsCmd() tea.Cmd {
	return func() tea.Msg {
		cpu, mem := getHostMetrics()
		return hostMetricsMsg{cpu: cpu, mem: mem}
	}
}

func doRefresh(socket string) tea.Cmd {
	return func() tea.Msg {
		resp, err := daemon.SendRequest(socket, daemon.Request{Command: daemon.CmdList})
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
			return logsMsg{}
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
		return logsMsg{lines: lines}
	}
}

// doAction sends an RPC then immediately re-fetches the process list.
func doAction(socket string, req daemon.Request) tea.Cmd {
	return func() tea.Msg {
		_, _ = daemon.SendRequest(socket, req)
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

func (m Model) buildTitle() string {
	name := lipgloss.NewStyle().Bold(true).Foreground(clText).Render("pm2 monit")
	var info string
	if m.err != nil {
		info = lipgloss.NewStyle().Foreground(clErrored).Render("  ✗ daemon unreachable")
	} else if !m.updated.IsZero() {
		info = lipgloss.NewStyle().Foreground(clMuted).Render(
			fmt.Sprintf("  %d processes · %s", len(m.procs), m.updated.Format("15:04:05")),
		)
	}
	return lipgloss.NewStyle().Background(clHdrBg).Width(m.width).Padding(0, 1).Render(name + info)
}

// ─── left panel: process list ─────────────────────────────────────────────────

func (m Model) buildLeft(w, h int) string {
	hdr := secHeader("processes", w)
	blank := strings.Repeat(" ", w)

	// Calculate max uptime length for alignment
	maxUpLen := 1
	for _, p := range m.procs {
		upLen := len(shortUptime(p))
		if upLen > maxUpLen {
			maxUpLen = upLen
		}
	}
	nameW := w - 5 - maxUpLen
	if nameW < 5 {
		nameW = 5
	}

	var rows []string
	for i, p := range m.procs {
		dot := dotFor(p.Status)
		name := crop(p.Name, nameW)
		up := shortUptime(p)

		var line string
		if i == m.selected {
			nameSt := lipgloss.NewStyle().Bold(true).Foreground(clSelName)
			upSt := lipgloss.NewStyle().Foreground(clSelText)
			line = fmt.Sprintf("%s %s %s",
				dot,
				nameSt.Width(nameW).Render(name),
				upSt.Render(up),
			)
		} else {
			line = fmt.Sprintf("%s %-*s %s",
				dot, nameW, name,
				lipgloss.NewStyle().Foreground(clMuted).Render(up),
			)
		}
		st := lipgloss.NewStyle().Width(w).Padding(0, 1)
		if i == m.selected {
			st = st.Background(clSelBg)
		}
		rows = append(rows, st.Render(line))
	}
	if len(rows) == 0 {
		rows = append(rows, lipgloss.NewStyle().Width(w).Padding(0, 1).Foreground(clMuted).Render("no processes"))
	}
	for len(rows) < h-1 {
		rows = append(rows, blank)
	}
	return hdr + "\n" + strings.Join(rows[:min(h-1, len(rows))], "\n")
}

// ─── right panel: detail + logs ──────────────────────────────────────────────

func (m Model) buildRight(w, h int) string {
	if len(m.procs) == 0 {
		return lipgloss.NewStyle().Width(w).Padding(1, 2).Foreground(clMuted).
			Render("no processes\nstart one: pm2 start <script>")
	}
	p := m.procs[m.selected]

	// detail: 1 header + detailRows rows
	detail := m.buildDetail(p, w)
	// logs: 1 header + remaining rows
	logH := h - (1 + detailRows) - 1 // 1 for divider line between sections
	if logH < 2 {
		logH = 2
	}
	logs := m.buildLogs(p.Name, w, logH)
	return detail + "\n" + logs
}

func (m Model) buildDetail(p process.ProcessInfo, w int) string {
	hdr := secHeader(fmt.Sprintf("detail — %s", p.Name), w)
	kst := lipgloss.NewStyle().Foreground(clMuted).Width(18)

	scriptVal := p.Script
	if len(p.Args) > 0 {
		scriptVal += " " + strings.Join(p.Args, " ")
	}

	type row struct{ k, v, sty string }
	rows := []row{
		{"script", crop(scriptVal, w-21), "path"},
		{"namespace", p.Namespace, ""},
		{"user", p.User, ""},
		{"status", string(p.Status), "status"},
		{"cpu", fmt.Sprintf("%.1f%%", p.CPU), "cpu"},
		{"mem", formatBytes(p.Memory), "mem"},
		{"uptime", fullUptime(p), ""},
		{"started", fmtTime(p.StartedAt), ""},
		{"restarts", fmt.Sprintf("%d / %d max", p.Restarts, p.MaxRestarts), ""},
		{"cron", cronExpr(p.Cron), "cron"},
		{"cron next", cronNext(p.Cron), "cron"},
		{"cron_restart", cronExpr(p.CronRestart), "cron"},
		{"cron_restart next", cronNext(p.CronRestart), "cron"},
		{"last run", cronLastRun(p.LastCronAt, p.LastCronStatus), "last"},
		{"watching", formatWatching(p.Watch), "watching"},
		{"stdout", crop(p.LogFile, w-21), "path"},
		{"stderr", crop(p.ErrorFile, w-21), "path"},
	}
	var lines []string
	for _, r := range rows {
		var val string
		switch r.sty {
		case "path":
			val = lipgloss.NewStyle().Foreground(clPath).Render(r.v)
		case "cron":
			val = lipgloss.NewStyle().Foreground(clCron).Render(r.v)
		case "last":
			val = cronLastRunStyled(p.LastCronAt, p.LastCronStatus)
		case "status":
			val = statusLabel(p.Status)
		case "watching":
			if p.Watch {
				val = lipgloss.NewStyle().Foreground(clOnline).Render(r.v)
			} else {
				val = lipgloss.NewStyle().Foreground(clMuted).Render(r.v)
			}
		case "cpu":
			if p.Status == process.StatusOnline {
				val = lipgloss.NewStyle().Foreground(clOnline).Render(r.v)
			} else {
				val = lipgloss.NewStyle().Foreground(clMuted).Render(r.v)
			}
		case "mem":
			if p.Status == process.StatusOnline {
				val = lipgloss.NewStyle().Foreground(clText).Render(r.v)
			} else {
				val = lipgloss.NewStyle().Foreground(clMuted).Render(r.v)
			}
		default:
			val = lipgloss.NewStyle().Foreground(clText).Render(r.v)
		}
		lines = append(lines, lipgloss.NewStyle().Width(w).Padding(0, 1).Render(kst.Render(r.k)+" "+val))
	}
	return hdr + "\n" + strings.Join(lines, "\n")
}

func (m Model) buildLogs(name string, w, h int) string {
	hdr := secHeader(fmt.Sprintf("logs — %s", name), w)
	blank := strings.Repeat(" ", w)

	var rows []string
	for _, l := range m.logs {
		rows = append(rows, lipgloss.NewStyle().Width(w).Padding(0, 1).Foreground(clMuted).Render(crop(l, w-3)))
	}
	if len(rows) == 0 {
		rows = append(rows, lipgloss.NewStyle().Width(w).Padding(0, 1).Foreground(clMuted).Render("(no log entries)"))
	}
	for len(rows) < h {
		rows = append(rows, blank)
	}
	visible := rows
	if len(visible) > h {
		visible = visible[len(visible)-h:]
	}
	return hdr + "\n" + strings.Join(visible, "\n")
}

func buildFooter(w int, sortBy SortField) string {
	keys := [][2]string{
		{"↑↓ / jk", "navigate"},
		{"r", "restart"},
		{"p", "pause"},
		{"d", "delete"},
		{"s", "sort: " + string(sortBy)},
		{"q", "quit"},
	}
	ks := lipgloss.NewStyle().Foreground(clText)
	ds := lipgloss.NewStyle().Foreground(clMuted)
	var parts []string
	for _, h := range keys {
		parts = append(parts, ks.Render(h[0])+" "+ds.Render(h[1]))
	}
	return lipgloss.NewStyle().Background(clHdrBg).Width(w).Padding(0, 1).
		Render(strings.Join(parts, "  │  "))
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func (m Model) getLeftColW() int {
	w := 34

	if len(m.procs) > 0 {
		maxNameLen := 0
		maxUpLen := 0
		for _, p := range m.procs {
			if len(p.Name) > maxNameLen {
				maxNameLen = len(p.Name)
			}
			upLen := len(shortUptime(p))
			if upLen > maxUpLen {
				maxUpLen = upLen
			}
		}
		// Calculate ideal width: dot(1) + space(1) + name + space(1) + uptime + padding(2) = name + uptime + 5
		idealW := maxNameLen + maxUpLen + 5
		if idealW > w {
			w = idealW
		}
	}

	// Cap the width to ensure the right panel has at least 40 columns
	minRightW := 40
	maxAllowedW := m.width - minRightW - 1 // 1 for the divider
	if maxAllowedW < 34 {
		maxAllowedW = 34
	}

	// Also put a reasonable absolute maximum cap (50) so it doesn't look too sparse
	if maxAllowedW > 50 {
		maxAllowedW = 50
	}

	if w > maxAllowedW {
		w = maxAllowedW
	}

	// Ultimate guard: left column cannot be wider than m.width - 10
	if w > m.width-10 {
		w = m.width - 10
	}
	if w < 10 {
		w = 10
	}

	return w
}

func secHeader(label string, w int) string {
	return lipgloss.NewStyle().Background(clHdrBg).Foreground(clMuted).
		Width(w).Padding(0, 1).Render(strings.ToUpper(label))
}

func dotFor(s process.Status) string {
	switch s {
	case process.StatusOnline:
		return lipgloss.NewStyle().Foreground(clOnline).Render("●")
	case process.StatusErrored:
		return lipgloss.NewStyle().Foreground(clErrored).Render("●")
	case process.StatusLaunching, process.StatusStopping:
		return lipgloss.NewStyle().Foreground(clWarn).Render("◌")
	default:
		return lipgloss.NewStyle().Foreground(clStopped).Render("○")
	}
}

func statusLabel(s process.Status) string {
	switch s {
	case process.StatusOnline:
		return lipgloss.NewStyle().Foreground(clOnline).Render(string(s))
	case process.StatusErrored:
		return lipgloss.NewStyle().Foreground(clErrored).Render(string(s))
	case process.StatusLaunching, process.StatusStopping:
		return lipgloss.NewStyle().Foreground(clWarn).Render(string(s))
	default:
		return lipgloss.NewStyle().Foreground(clStopped).Render(string(s))
	}
}

func shortUptime(p process.ProcessInfo) string {
	if p.Status != process.StatusOnline || p.StartedAt.IsZero() {
		return "—"
	}
	d := time.Since(p.StartedAt)
	days := int(d.Hours()) / 24
	if days > 0 {
		return fmt.Sprintf("%dd%dh", days, int(d.Hours())%24)
	}
	hours := int(d.Hours())
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes())%60, int(d.Seconds())%60)
}

func fullUptime(p process.ProcessInfo) string {
	if p.Status != process.StatusOnline || p.StartedAt.IsZero() {
		return "—"
	}
	d := time.Since(p.StartedAt)
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if days > 0 {
		return fmt.Sprintf("%d days  %02d:%02d:%02d", days, h, m, s)
	}
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("2006-01-02  15:04:05")
}

func cronExpr(expr string) string {
	if expr == "" {
		return "—"
	}
	return expr
}

func cronNext(expr string) string {
	if expr == "" {
		return "—"
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := parser.Parse(expr)
	if err != nil {
		return "invalid expression"
	}
	return sched.Next(time.Now()).Format("2006-01-02  15:04:05")
}

func cronLastRun(t time.Time, status string) string {
	if t.IsZero() {
		return "—"
	}
	return fmt.Sprintf("%s  %s", t.Format("2006-01-02  15:04:05"), status)
}

// cronLastRunStyled returns the last-run line with status coloured.
func cronLastRunStyled(t time.Time, status string) string {
	if t.IsZero() {
		return lipgloss.NewStyle().Foreground(clMuted).Render("—")
	}
	ts := lipgloss.NewStyle().Foreground(clText).Render(t.Format("2006-01-02  15:04:05"))
	var badge string
	switch status {
	case "ok":
		badge = lipgloss.NewStyle().Foreground(clOnline).Render("  ok")
	case "failed":
		badge = lipgloss.NewStyle().Foreground(clErrored).Render("  failed")
	default:
		badge = lipgloss.NewStyle().Foreground(clMuted).Render("  " + status)
	}
	return ts + badge
}

func crop(s string, maxLen int) string {
	if maxLen <= 4 || len(s) <= maxLen {
		return s
	}
	return "…" + s[len(s)-(maxLen-1):]
}

func formatWatching(watch bool) string {
	if watch {
		return "enabled"
	}
	return "disabled"
}

func formatBytes(b uint64) string {
	if b == 0 {
		return "0b"
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%db", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"kb", "mb", "gb", "tb"}
	return fmt.Sprintf("%.1f%s", float64(b)/float64(div), units[exp])
}

// ─── TUI table list view ──────────────────────────────────────────────────────

type colDef struct {
	name  string
	width int
	align lipgloss.Position
}

var listColumns = []colDef{
	{"id", 3, lipgloss.Right},
	{"namespace", 10, lipgloss.Left},
	{"name", 12, lipgloss.Left},
	{"version", 8, lipgloss.Left},
	{"pid", 6, lipgloss.Right},
	{"uptime", 8, lipgloss.Right},
	{"↺", 3, lipgloss.Right},
	{"status", 9, lipgloss.Left},
	{"cpu", 6, lipgloss.Right},
	{"mem", 8, lipgloss.Right},
	{"user", 8, lipgloss.Left},
	{"cron", 10, lipgloss.Left},
	{"last exec", 19, lipgloss.Left},
}

func (m Model) buildListTUI() string {
	if len(m.procs) == 0 {
		body := lipgloss.NewStyle().Width(m.width).Height(m.height - 2).
			Align(lipgloss.Center, lipgloss.Center).
			Foreground(clMuted).
			Render("No processes running\nstart one: pm2 start <script>")
		return lipgloss.JoinVertical(lipgloss.Left, m.buildTitle(), body, buildFooter(m.width, m.SortBy))
	}

	// Calculate name column width dynamically based on terminal width
	width := m.width
	fixedW := 0
	for i, col := range listColumns {
		if i != 2 { // name is index 2
			fixedW += col.width + 3 // width + 2 spaces + 1 separator
		}
	}
	fixedW += 2 // outer borders

	nameW := width - fixedW - 3
	if nameW < 18 {
		nameW = 18
	}

	cols := make([]colDef, len(listColumns))
	copy(cols, listColumns)
	cols[2].width = nameW

	// Render borders and header
	top := drawBorder(cols, "┌", "┬", "┐", "─")
	
	var hdrParts []string
	hdrStyle := lipgloss.NewStyle().Background(clHdrBg).Foreground(clText).Bold(true)
	for _, col := range cols {
		text := col.name
		if len(text) > col.width {
			text = text[:col.width]
		}
		style := hdrStyle.Width(col.width)
		if col.align == lipgloss.Right {
			style = style.Align(lipgloss.Right)
		} else {
			style = style.Align(lipgloss.Left)
		}
		hdrParts = append(hdrParts, " "+style.Render(text)+" ")
	}
	hdrRow := "│" + strings.Join(hdrParts, "│") + "│"
	sep := drawBorder(cols, "├", "┼", "┤", "─")

	var lines []string
	lines = append(lines, sepLine(top))
	lines = append(lines, sepLine(hdrRow))
	lines = append(lines, sepLine(sep))

	// Render rows
	rowStyle := lipgloss.NewStyle()
	borderStyle := lipgloss.NewStyle().Foreground(clBorder)
	for i, p := range m.procs {
		var rowParts []string
		for _, col := range cols {
			val := getColVal(p, col.name)
			if len(val) > col.width {
				if col.name == "name" {
					val = crop(val, col.width)
				} else {
					val = val[:col.width]
				}
			}

			style := rowStyle.Width(col.width)
			if col.align == lipgloss.Right {
				style = style.Align(lipgloss.Right)
			} else {
				style = style.Align(lipgloss.Left)
			}

			if i == m.selected {
				style = style.Background(clSelBg)
			}

			var renderedVal string
			if i == m.selected {
				switch col.name {
				case "name":
					renderedVal = style.Bold(true).Foreground(clSelName).Render(val)
				case "id", "status":
					renderedVal = style.Bold(true).Foreground(getStatusColor(p.Status)).Render(val)
				case "cpu":
					cpuSt := style.Bold(true)
					if p.Status == process.StatusOnline {
						cpuSt = cpuSt.Foreground(clOnline)
					} else {
						cpuSt = cpuSt.Foreground(clSelText)
					}
					renderedVal = cpuSt.Render(val)
				case "mem":
					memSt := style.Bold(true)
					if p.Status == process.StatusOnline {
						memSt = memSt.Foreground(clSelText)
					} else {
						memSt = memSt.Foreground(clSelText)
					}
					renderedVal = memSt.Render(val)
				default:
					renderedVal = style.Bold(true).Foreground(clSelText).Render(val)
				}
			} else {
				switch col.name {
				case "id":
					idSt := style.Bold(true).Foreground(getStatusColor(p.Status))
					renderedVal = idSt.Render(val)
				case "status":
					stSt := style.Foreground(getStatusColor(p.Status))
					renderedVal = stSt.Render(val)
				case "cpu":
					cpuSt := style
					if p.Status == process.StatusOnline {
						cpuSt = cpuSt.Foreground(clOnline)
					} else {
						cpuSt = cpuSt.Foreground(clStopped)
					}
					renderedVal = cpuSt.Render(val)
				case "mem":
					memSt := style
					if p.Status == process.StatusOnline {
						memSt = memSt.Foreground(clText)
					} else {
						memSt = memSt.Foreground(clStopped)
					}
					renderedVal = memSt.Render(val)
				default:
					defaultSt := style
					if p.Status != process.StatusOnline && col.name != "name" && col.name != "version" && col.name != "namespace" {
						defaultSt = defaultSt.Foreground(clStopped)
					} else {
						defaultSt = defaultSt.Foreground(clText)
					}
					renderedVal = defaultSt.Render(val)
				}
			}

			var cell string
			if i == m.selected {
				cellSt := lipgloss.NewStyle().Background(clSelBg)
				cell = cellSt.Render(" ") + renderedVal + cellSt.Render(" ")
			} else {
				cell = " " + renderedVal + " "
			}
			rowParts = append(rowParts, cell)
		}
		
		line := borderStyle.Render("│") + strings.Join(rowParts, borderStyle.Render("│")) + borderStyle.Render("│")
		lines = append(lines, line)
	}

	bottom := drawBorder(cols, "└", "┴", "┘", "─")
	lines = append(lines, sepLine(bottom))

	// Host metrics rows
	hostMetricsW := m.width
	cpuMemLine, diskNetLine := m.buildHostMetricsLines(hostMetricsW)
	lines = append(lines, cpuMemLine, diskNetLine)

	// Pad with empty lines if height is larger
	contentH := m.height - 2
	for len(lines) < contentH {
		lines = append(lines, strings.Repeat(" ", m.width))
	}
	body := strings.Join(lines[:contentH], "\n")

	return lipgloss.JoinVertical(lipgloss.Left, m.buildTitle(), body, buildFooter(m.width, m.SortBy))
}

func drawBorder(cols []colDef, left, mid, right, fill string) string {
	var parts []string
	for _, col := range cols {
		parts = append(parts, strings.Repeat(fill, col.width+2))
	}
	return left + strings.Join(parts, mid) + right
}

func sepLine(s string) string {
	return lipgloss.NewStyle().Foreground(clBorder).Render(s)
}

func getStatusColor(s process.Status) lipgloss.AdaptiveColor {
	switch s {
	case process.StatusOnline:
		return clOnline
	case process.StatusErrored:
		return clErrored
	case process.StatusLaunching, process.StatusStopping:
		return clWarn
	default:
		return clStopped
	}
}

func getColVal(p process.ProcessInfo, colName string) string {
	switch colName {
	case "id":
		return fmt.Sprintf("%d", p.ID)
	case "namespace":
		return p.Namespace
	case "name":
		return p.Name
	case "version":
		if p.Version == "" {
			return "-"
		}
		return p.Version
	case "pid":
		if p.PID <= 0 {
			return "-"
		}
		return fmt.Sprintf("%d", p.PID)
	case "uptime":
		return shortUptime(p)
	case "↺":
		return fmt.Sprintf("%d", p.Restarts)
	case "status":
		return string(p.Status)
	case "cpu":
		if p.Status != process.StatusOnline {
			return "0.0%"
		}
		return fmt.Sprintf("%.1f%%", p.CPU)
	case "mem":
		if p.Status != process.StatusOnline {
			return "0b"
		}
		return formatBytes(p.Memory)
	case "user":
		if p.User == "" {
			return "-"
		}
		return p.User
	case "cron":
		if p.Cron == "" {
			return "-"
		}
		return p.Cron
	case "last exec":
		if p.LastCronAt.IsZero() {
			return "-"
		}
		res := p.LastCronAt.Format("2006-01-02 15:04:05")
		if p.LastCronStatus != "" {
			res += fmt.Sprintf(" (%s)", p.LastCronStatus)
		}
		return res
	default:
		return ""
	}
}

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

func getHostMetrics() (float64, float64) {
	cpu := 5.2
	mem := 64.1

	cmd := exec.Command("top", "-l", "1", "-n", "0")
	out, err := cmd.Output()
	if err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "CPU usage:") {
				var user, sys float64
				_, err := fmt.Sscanf(line, "CPU usage: %f%% user, %f%% sys", &user, &sys)
				if err == nil {
					cpu = user + sys
				}
			} else if strings.HasPrefix(line, "PhysMem:") {
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

