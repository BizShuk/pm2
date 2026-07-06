package views

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/bizshuk/pm2/tui/theme"
)

// RenderNamespaceBar renders the one-row namespace switcher rendered
// between the header and the process list. The cursor chip is
// highlighted; when the chip strip is wider than the screen, a window
// slides to keep the cursor visible with ‹ / › overflow hints.
//
// The controller (tui.Model) is the source of ctx.Namespaces and
// ctx.NsCursor. This function is pure — it draws the row and never
// reaches back into the controller.
func RenderNamespaceBar(ctx ViewContext, w int) string {
	ns := ctx.Namespaces
	if len(ns) == 0 {
		ns = []string{"All"}
	}
	cursor := ctx.NsCursor
	if cursor < 0 || cursor >= len(ns) {
		cursor = 0
	}

	const sep = " · "
	sepW := runewidth.StringWidth(sep)
	const pad = 2 // 1-char horizontal padding on each side
	arrowW := runewidth.StringWidth("›") // ‹ is the same width

	chipW := make([]int, len(ns))
	for i, n := range ns {
		chipW[i] = runewidth.StringWidth(n)
	}

	// windowWidth is the sum of chip widths plus the separator overhead
	// for a half-open window [start, end). The selected chip's exact
	// rendered width is the same as its measured width — it just gets a
	// different style.
	windowWidth := func(start, end int) int {
		if end <= start {
			return 0
		}
		total := 0
		for i := start; i < end; i++ {
			total += chipW[i]
		}
		if end-start > 1 {
			total += (end - start - 1) * sepW
		}
		return total
	}

	n := len(ns)
	// budgetFor returns the available content width for a window once
	// padding + any overflow indicators are accounted for.
	budgetFor := func(start, end int) int {
		avail := w - pad
		if start > 0 {
			avail -= arrowW
		}
		if end < n {
			avail -= arrowW
		}
		return avail
	}
	_ = sepW

	// Best window: start wide enough that the full row fits without arrows;
	// otherwise expand outward from the cursor as far as the budget allows.
	start, end := 0, n
	if windowWidth(0, n)+pad > w {
		start, end = cursor, cursor+1
		// Round 1: grow left as far as possible.
		for s := cursor - 1; s >= 0; s-- {
			if windowWidth(s, end)+pad <= budgetFor(s, end) {
				start = s
			} else {
				break
			}
		}
		// Round 2: grow right as far as possible from current start.
		for e := end + 1; e <= n; e++ {
			if windowWidth(start, e)+pad <= budgetFor(start, e) {
				end = e
			} else {
				break
			}
		}
		// Round 3: revisit left — widening right may have created room.
		for s := start - 1; s >= 0; s-- {
			if windowWidth(s, end)+pad <= budgetFor(s, end) {
				start = s
			} else {
				break
			}
		}
	}

	showLeft := start > 0
	showRight := end < n
	return renderChips(ns, start, end, showLeft, showRight, cursor, w)
}

// renderChips paints the chips for a chosen window. The cursor is
// highlighted via the SelBg/SelText theme pair so it visually matches
// the row selection in the process list below.
func renderChips(ns []string, start, end int, showLeft, showRight bool, cursor, w int) string {
	selStyle := lipgloss.NewStyle().Bold(true).Background(theme.SelBg).Foreground(theme.SelText)
	textStyle := lipgloss.NewStyle().Foreground(theme.Text)
	mutedStyle := lipgloss.NewStyle().Foreground(theme.Muted)

	var parts []string
	if showLeft {
		parts = append(parts, mutedStyle.Render("‹"))
	}
	for i := start; i < end; i++ {
		if i > start {
			parts = append(parts, mutedStyle.Render(" · "))
		}
		if i == cursor {
			parts = append(parts, selStyle.Render(ns[i]))
		} else {
			parts = append(parts, textStyle.Render(ns[i]))
		}
	}
	if showRight {
		parts = append(parts, mutedStyle.Render("›"))
	}

	line := strings.Join(parts, "")
	bg := lipgloss.NewStyle().Background(theme.HdrBg)
	return bg.Width(w).Padding(0, 1).Render(line)
}
