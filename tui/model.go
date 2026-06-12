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
	"github.com/robfig/cron/v3"

	"github.com/shuk/pm2/daemon"
	"github.com/shuk/pm2/process"
)

const (
	leftColW   = 34
	refreshDur = 2 * time.Second
	maxLogTail = 14
	detailRows = 11 // rows in detail section (excluding header)
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
	clSelBg   = lipgloss.AdaptiveColor{Light: "#dbeafe", Dark: "#1c2333"}
	clHdrBg   = lipgloss.AdaptiveColor{Light: "#f1f5f9", Dark: "#161b22"}
	clBorder  = lipgloss.AdaptiveColor{Light: "#cbd5e1", Dark: "#30363d"}
	clText    = lipgloss.AdaptiveColor{Light: "#0f172a", Dark: "#e6edf3"}
)

// ─── messages ────────────────────────────────────────────────────────────────

type tickMsg time.Time
type refreshMsg struct {
	procs []process.ProcessInfo
	err   error
}
type logsMsg struct{ lines []string }

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
}

func New(socket string) Model {
	return Model{socket: socket, width: 120, height: 30}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		doRefresh(m.socket),
		tea.Tick(refreshDur, func(t time.Time) tea.Msg { return tickMsg(t) }),
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
			m.procs = msg.procs
			m.updated = time.Now()
			if m.selected >= len(m.procs) {
				m.selected = max(0, len(m.procs)-1)
			}
			if len(m.procs) > 0 {
				return m, readLogs(m.procs[m.selected].LogFile)
			}
		}

	case logsMsg:
		m.logs = msg.lines

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
	if len(m.procs) == 0 {
		return m, nil
	}
	targetID := fmt.Sprintf("%d", m.procs[m.selected].ID)
	switch msg.String() {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
			return m, readLogs(m.procs[m.selected].LogFile)
		}
	case "down", "j":
		if m.selected < len(m.procs)-1 {
			m.selected++
			return m, readLogs(m.procs[m.selected].LogFile)
		}
	case "r":
		return m, doAction(m.socket, daemon.Request{Command: daemon.CmdRestart, Name: targetID})
	case "s":
		return m, doAction(m.socket, daemon.Request{Command: daemon.CmdStop, Name: targetID})
	case "d":
		return m, doAction(m.socket, daemon.Request{Command: daemon.CmdDelete, Name: targetID})
	}
	return m, nil
}

// ─── commands ────────────────────────────────────────────────────────────────

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

	contentH := m.height - 2 // subtract title + footer rows
	rw := m.width - leftColW - 1

	left := lipgloss.NewStyle().Width(leftColW).Height(contentH).
		Render(m.buildLeft(leftColW, contentH))

	div := lipgloss.NewStyle().Width(1).Height(contentH).Foreground(clBorder).
		Render(strings.Repeat("│\n", contentH-1) + "│")

	right := lipgloss.NewStyle().Width(rw).Height(contentH).
		Render(m.buildRight(rw, contentH))

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, div, right)
	return lipgloss.JoinVertical(lipgloss.Left, m.buildTitle(), body, buildFooter(m.width))
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

	var rows []string
	for i, p := range m.procs {
		dot := dotFor(p.Status)
		name := crop(p.Name, w-16)
		up := shortUptime(p)

		line := fmt.Sprintf("%s %-*s %s",
			dot, w-16, name,
			lipgloss.NewStyle().Foreground(clMuted).Render(up),
		)
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
	kst := lipgloss.NewStyle().Foreground(clMuted).Width(10)

	type row struct{ k, v, sty string }
	rows := []row{
		{"script", crop(p.Script, w-13), "path"},
		{"namespace", p.Namespace, ""},
		{"status", string(p.Status), "status"},
		{"uptime", fullUptime(p), ""},
		{"started", fmtTime(p.StartedAt), ""},
		{"restarts", fmt.Sprintf("%d / %d max", p.Restarts, p.MaxRestarts), ""},
		{"cron", cronExpr(p.CronRestart), "cron"},
		{"cron next", cronNext(p.CronRestart), "cron"},
		{"last run", cronLastRun(p.LastCronAt, p.LastCronStatus), "last"},
		{"stdout", crop(p.LogFile, w-13), "path"},
		{"stderr", crop(p.ErrorFile, w-13), "path"},
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

func buildFooter(w int) string {
	keys := [][2]string{{"↑↓ / jk", "navigate"}, {"r", "restart"}, {"s", "stop"}, {"d", "delete"}, {"q", "quit"}}
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
