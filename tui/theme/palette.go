// Package theme is the single source of truth for the pm2 monitor
// colour palette. Every view in tui/views and every controller helper
// in tui reads its AdaptiveColor values from here.
//
// Keeping the palette in its own sub-package avoids a tui ↔ tui/views
// import cycle while still letting both reach the same colours.
package theme

import "github.com/charmbracelet/lipgloss"

// AdaptiveColor palette. Add new entries here when introducing a new
// semantic role (e.g. "warning"); never create new lipgloss.AdaptiveColor
// literals inside view code.
var (
	Online  = lipgloss.AdaptiveColor{Light: "#16a34a", Dark: "#3fb950"}
	Stopped = lipgloss.AdaptiveColor{Light: "#64748b", Dark: "#6e7681"}
	Errored = lipgloss.AdaptiveColor{Light: "#dc2626", Dark: "#f85149"}
	Warn    = lipgloss.AdaptiveColor{Light: "#d97706", Dark: "#e3b341"}
	Cron    = lipgloss.AdaptiveColor{Light: "#7c3aed", Dark: "#d2a8ff"}
	Path    = lipgloss.AdaptiveColor{Light: "#1d4ed8", Dark: "#388bfd"}
	Muted   = lipgloss.AdaptiveColor{Light: "#64748b", Dark: "#8b949e"}
	SelBg   = lipgloss.AdaptiveColor{Light: "#e0e7ff", Dark: "#2e3440"}
	SelName = lipgloss.AdaptiveColor{Light: "#0891b2", Dark: "#06b6d4"}
	SelText = lipgloss.AdaptiveColor{Light: "#0f172a", Dark: "#ffffff"}
	HdrBg   = lipgloss.AdaptiveColor{Light: "#f1f5f9", Dark: "#161b22"}
	Border  = lipgloss.AdaptiveColor{Light: "#cbd5e1", Dark: "#30363d"}
	Text    = lipgloss.AdaptiveColor{Light: "#0f172a", Dark: "#e6edf3"}
)
