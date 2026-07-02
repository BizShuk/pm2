package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/robfig/cron/v3"

	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/tui/theme"
)

// ─── uptime / time / cron formatters (pure functions) ────────────────────────

// shortUptime renders uptime as "XdYh" / "XhYm" / "XmYs" depending on size.
// Returns "—" when the process is not online or has no started-at timestamp.
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

// fullUptime renders uptime as "Xd HH:MM:SS" / "HH:MM:SS" depending on size.
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

// fmtTime formats t as "YYYY-MM-DD  HH:MM:SS"; returns "—" for zero time.
func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("2006-01-02  15:04:05")
}

// cronExpr returns the cron expression unchanged, or "—" if empty.
func cronExpr(expr string) string {
	if expr == "" {
		return "—"
	}
	return expr
}

// cronNext returns the next scheduled fire time formatted as a datetime string.
// Returns "invalid expression" when expr fails to parse; "—" when expr is empty.
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

// cronLastRunStyled renders a last-run line with the status badge coloured.
func cronLastRunStyled(t time.Time, status string, maxStatusLen int) string {
	if t.IsZero() {
		return lipgloss.NewStyle().Foreground(theme.Muted).Render("—")
	}
	ts := lipgloss.NewStyle().Foreground(theme.Text).Render(t.Format("2006-01-02  15:04:05"))
	if maxStatusLen < 5 {
		maxStatusLen = 5
	}
	status = CropRight(status, maxStatusLen)
	var badge string
	switch status {
	case "ok":
		badge = lipgloss.NewStyle().Foreground(theme.Online).Render("  ok")
	case "failed":
		badge = lipgloss.NewStyle().Foreground(theme.Errored).Render("  failed")
	default:
		badge = lipgloss.NewStyle().Foreground(theme.Muted).Render("  " + status)
	}
	return ts + badge
}

// ─── cropping helpers (runewidth-aware) ─────────────────────────────────────

// Crop returns the tail of s with a leading "…" so the rendered width ≤ maxLen.
// Width is measured in runes via runewidth (CJK double-width).
func Crop(s string, maxLen int) string {
	if maxLen <= 4 {
		return s
	}
	sw := runewidth.StringWidth(s)
	if sw <= maxLen {
		return s
	}
	runes := []rune(s)
	width := 0
	targetWidth := maxLen - 1 // 1 for the ellipsis "…"
	startIndex := len(runes)
	for i := len(runes) - 1; i >= 0; i-- {
		rw := runewidth.RuneWidth(runes[i])
		if width+rw > targetWidth {
			break
		}
		width += rw
		startIndex = i
	}
	return "…" + string(runes[startIndex:])
}

// CropRight returns the head of s with a trailing "…" so the rendered width ≤ maxLen.
func CropRight(s string, maxLen int) string {
	if maxLen <= 4 {
		return s
	}
	sw := runewidth.StringWidth(s)
	if sw <= maxLen {
		return s
	}
	runes := []rune(s)
	width := 0
	targetWidth := maxLen - 1 // 1 for the ellipsis "…"
	var result []rune
	for _, r := range runes {
		rw := runewidth.RuneWidth(r)
		if width+rw > targetWidth {
			break
		}
		width += rw
		result = append(result, r)
	}
	return string(result) + "…"
}

// ─── byte / boolean / section-header helpers ─────────────────────────────────

// formatWatching renders the Watch toggle as "enabled" / "disabled".
func formatWatching(watch bool) string {
	if watch {
		return "enabled"
	}
	return "disabled"
}

// formatBytes renders a byte count in human units (kb/mb/gb/tb).
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

// secHeader renders a section header bar with the given width.
func secHeader(label string, w int) string {
	cropped := CropRight(label, w-2)
	return lipgloss.NewStyle().Background(theme.HdrBg).Foreground(theme.Muted).
		Width(w).Padding(0, 1).Render(strings.ToUpper(cropped))
}

// dotFor returns the status glyph coloured by status.
func dotFor(s process.Status) string {
	switch s {
	case process.StatusOnline:
		return lipgloss.NewStyle().Foreground(theme.Online).Render("●")
	case process.StatusErrored:
		return lipgloss.NewStyle().Foreground(theme.Errored).Render("●")
	case process.StatusLaunching, process.StatusStopping:
		return lipgloss.NewStyle().Foreground(theme.Warn).Render("◌")
	case process.StatusPaused:
		return lipgloss.NewStyle().Foreground(theme.Cron).Render("⏸")
	default:
		return lipgloss.NewStyle().Foreground(theme.Stopped).Render("○")
	}
}

// statusLabel returns the status string itself, coloured by status.
func statusLabel(s process.Status) string {
	switch s {
	case process.StatusOnline:
		return lipgloss.NewStyle().Foreground(theme.Online).Render(string(s))
	case process.StatusErrored:
		return lipgloss.NewStyle().Foreground(theme.Errored).Render(string(s))
	case process.StatusLaunching, process.StatusStopping:
		return lipgloss.NewStyle().Foreground(theme.Warn).Render(string(s))
	case process.StatusPaused:
		return lipgloss.NewStyle().Foreground(theme.Cron).Render(string(s))
	default:
		return lipgloss.NewStyle().Foreground(theme.Stopped).Render(string(s))
	}
}

// getStatusColor returns the lipgloss.AdaptiveColor associated with a status.
// It is the single source of truth for status→color mapping (also used by list rows).
func getStatusColor(s process.Status) lipgloss.AdaptiveColor {
	switch s {
	case process.StatusOnline:
		return theme.Online
	case process.StatusErrored:
		return theme.Errored
	case process.StatusLaunching, process.StatusStopping:
		return theme.Warn
	case process.StatusPaused:
		return theme.Cron
	default:
		return theme.Stopped
	}
}