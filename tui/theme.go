// Package tui theme bridge. The colour palette lives in the
// tui/theme sub-package (so the tui/views sub-package can import it
// without creating a cycle). This file re-exports the same values as
// package-level vars named `clXxx`, preserving the names used by the
// rest of the tui package.
package tui

import "github.com/bizshuk/pm2/tui/theme"

// Palette aliases — kept short and prefixed with `cl` for readability
// in view code. These are the same memory locations as the entries in
// tui/theme (they are package-level vars, not copies), so any future
// theme change applies to both the tui package and tui/views.
var (
	clOnline  = theme.Online
	clStopped = theme.Stopped
	clErrored = theme.Errored
	clWarn    = theme.Warn
	clCron    = theme.Cron
	clPath    = theme.Path
	clMuted   = theme.Muted
	clSelBg   = theme.SelBg
	clSelName = theme.SelName
	clSelText = theme.SelText
	clHdrBg   = theme.HdrBg
	clBorder  = theme.Border
	clText    = theme.Text
)