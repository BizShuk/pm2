package views

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// glyphsInUse is every non-ASCII character the renderers in this package
// emit as layout-bearing content. If a new one is introduced, add it here:
// the whole point of the list is that a glyph whose two measurements
// disagree breaks alignment only on someone else's machine.
var glyphsInUse = []string{
	"●", "○", "◌", "⏸", // status markers
	"│", "├", "┤", "┬", "┴", "┼", "┌", "┐", "└", "┘", "─", // table borders
	"↑", "↓", "→", "←", "‹", "›", "·", "…", "↺", // chrome and cropping
	"⇣", "⇡", "⇅", "█", "░", // dashboard host panel
	"—", "▸", "▾", "🔶", // detail dashes, tree, current-file marker
	"中", "資", "料", // wide characters must stay wide
}

func TestScreenAgreesWithLipgloss(t *testing.T) {
	// Crop/CropRight and the column arithmetic measure with `screen`;
	// lipgloss pads and truncates with its own table. A disagreement means
	// a pane sized by one engine and filled by the other, which is how a
	// row ends up one column too wide and wraps onto a second line.
	for _, glyph := range glyphsInUse {
		byScreen := screen.StringWidth(glyph)
		byLipgloss := lipgloss.Width(glyph)
		if byScreen != byLipgloss {
			t.Errorf("%q: screen says %d columns, lipgloss says %d — layout will break wherever the two are combined",
				glyph, byScreen, byLipgloss)
		}
	}
}

func TestScreenIgnoresTheAmbientLocale(t *testing.T) {
	// runewidth's package-level default flips to true under
	// LC_CTYPE=zh_TW.UTF-8 and doubles every ambiguous glyph. The
	// renderer holds its own Condition precisely so that cannot happen.
	if screen.EastAsianWidth {
		t.Fatal("screen.EastAsianWidth is true; views/width.go must pin it to false")
	}
	if got := screen.StringWidth("●"); got != 1 {
		t.Errorf("screen.StringWidth(\"●\") = %d, want 1 regardless of LC_CTYPE", got)
	}
	// The ambient default is whatever the locale made it — asserting it is
	// only meaningful as documentation of what we are insulating from.
	t.Logf("ambient runewidth.EastAsianWidth = %v", runewidth.EastAsianWidth)
}

func TestWideCharactersStayWide(t *testing.T) {
	// Pinning the ambiguous-width flag must not make CJK text measure as
	// narrow — a task named in Chinese still needs two columns per rune.
	if got := screen.StringWidth("資料同步"); got != 8 {
		t.Errorf("screen.StringWidth(\"資料同步\") = %d, want 8", got)
	}
	if got := CropRight("資料同步作業", 8); screen.StringWidth(got) > 8 {
		t.Errorf("CropRight returned %q (%d columns), want at most 8", got, screen.StringWidth(got))
	}
	if got := Crop("資料同步作業", 8); screen.StringWidth(got) > 8 {
		t.Errorf("Crop returned %q (%d columns), want at most 8", got, screen.StringWidth(got))
	}
}
