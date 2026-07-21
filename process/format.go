// Package process contains the data types shared between the CLI,
// the daemon, and the TUI. The format helpers in this file are the
// single source of truth for column rendering: both `pm2 monitor`
// (via tui/views/format.go wrappers) and `pm2 list` (via cmd/list.go)
// pull from the same primitives so the two views can never drift
// in how a process is presented.
//
// The functions are pure (no I/O, no lipgloss) so they can be used
// from either the lipgloss-aware tui package or the plain-stdout
// cmd package. Colour wrappers remain in tui/views.
package process

import (
	"fmt"
	"time"
)

// Dash is the placeholder used everywhere a cell is empty. Matches
// the tui/views/format.go convention so the two outputs read
// consistently.
const Dash = "—"

// ShortUptime renders uptime as "5m30s" / "2h15m" / "1d4h" — returns
// Dash when the process is not online or has no started-at
// timestamp. The TUI uses a wider "5m30s" form internally; this
// version is suitable for a non-monospace terminal column.
func ShortUptime(p ProcessInfo) string {
	if p.Status != StatusOnline || p.StartedAt.IsZero() {
		return Dash
	}
	d := time.Since(p.StartedAt).Truncate(time.Second)
	return shortDuration(d)
}

func shortDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	minutes := int(d / time.Minute)
	d -= time.Duration(minutes) * time.Minute
	seconds := int(d / time.Second)

	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

// FormatBytes renders a byte count in human units (1024-base).
// Returns "0b" for zero so the column reads consistently for
// idle processes.
func FormatBytes(b uint64) string {
	if b == 0 {
		return "0b"
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%db", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"kb", "mb", "gb", "tb"}
	return fmt.Sprintf("%.1f%s", float64(b)/float64(div), units[exp])
}

// CPUPercent renders the CPU% with one decimal. Returns "0.0%" when
// the process is not online so the column reads consistently.
func CPUPercent(p ProcessInfo) string {
	if p.Status != StatusOnline {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", p.CPU)
}

// MemCell pairs FormatBytes with the not-online dash. Monitor uses
// the same rule.
func MemCell(p ProcessInfo) string {
	if p.Status != StatusOnline {
		return "0b"
	}
	return FormatBytes(p.Memory)
}

// NamespaceOrDefault mirrors the empty-string normalisation in
// tui.Model.recomputeNamespaces — empty namespaces display
// "default".
func NamespaceOrDefault(ns string) string {
	if ns == "" {
		return "default"
	}
	return ns
}

// PIDOrDash shows "-" for processes that have not yet spawned a
// real PID (scheduled cron tasks, paused jobs).
func PIDOrDash(pid int) string {
	if pid <= 0 {
		return Dash
	}
	return fmt.Sprintf("%d", pid)
}

// VersionOrDash returns the AppConfig version or a dash.
func VersionOrDash(v string) string {
	if v == "" {
		return Dash
	}
	return v
}

// UserOrDash returns the AppConfig user (set by the daemon) or a
// dash.
func UserOrDash(u string) string {
	if u == "" {
		return Dash
	}
	return u
}

// CronOrDash returns the cron expression or a dash.
func CronOrDash(expr string) string {
	if expr == "" {
		return Dash
	}
	return expr
}

// LastExec renders the most-recent cron fire timestamp and status
// as a single "YYYY-MM-DD HH:MM:SS (status)" cell, or a dash if
// there has not been a fire yet. Mirrors the wide-table column.
func LastExec(p ProcessInfo) string {
	if p.LastCronAt.IsZero() {
		return Dash
	}
	res := p.LastCronAt.Format("2006-01-02 15:04:05")
	if p.LastCronStatus != "" {
		res += fmt.Sprintf(" (%s)", p.LastCronStatus)
	}
	return res
}
