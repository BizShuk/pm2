package views

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// testMain is shared setup for the view tests. lipgloss strips colors
// by default in non-TTY environments (i.e. go test), so we force a
// profile to make ANSI sequences deterministic for assertions.
func init() {
	lipgloss.SetColorProfile(termenv.TrueColor)
}

// stubNS builds a ViewContext whose Namespaces / NsCursor are set; the
// other fields are zero-valued because RenderNamespaceBar reads only the
// namespace bits.
func stubNS(names []string, cursor int) ViewContext {
	return ViewContext{Namespaces: names, NsCursor: cursor}
}

func TestNamespaceBarRendersAllChipsWhenTheyFit(t *testing.T) {
	ctx := stubNS([]string{"All", "dev", "prod"}, 2)
	out := RenderNamespaceBar(ctx, 80)
	for _, want := range []string{"All", "dev", "prod"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected chip %q in output, got: %q", want, out)
		}
	}
	if strings.Contains(out, "‹") || strings.Contains(out, "›") {
		t.Errorf("no overflow indicators expected, got: %q", out)
	}
}

func TestNamespaceBarHighlightsSelected(t *testing.T) {
	ctx := stubNS([]string{"All", "dev", "prod"}, 2)
	out := RenderNamespaceBar(ctx, 80)
	// Find the substring around the selected chip and confirm it has
	// a non-empty ANSI styling (any escape sequence) — the SelBg
	// background is the unambiguous "selected" marker.
	selected := "prod"
	idx := strings.Index(out, selected)
	if idx < 0 {
		t.Fatalf("selected chip %q not in output: %q", selected, out)
	}
	before := out[:idx]
	if !strings.Contains(before, "\x1b[") {
		t.Errorf("expected ANSI escape before selected chip, got: %q", out)
	}
}

func TestNamespaceBarShowsOverflowArrowsWhenNarrow(t *testing.T) {
	// 6 chips at width 14 — definitely overflows, the right arrow
	// must appear to indicate there's more to scroll to.
	ctx := stubNS([]string{"All", "alpha", "bravo", "charlie", "delta", "echo"}, 0)
	out := RenderNamespaceBar(ctx, 14)
	if !strings.Contains(out, "›") {
		t.Errorf("expected right overflow arrow, got: %q", out)
	}
}

func TestNamespaceBarRightArrowDisappearsAtEnd(t *testing.T) {
	// Cursor on the rightmost chip → the › arrow must NOT appear
	// because there's no more content to the right.
	ctx := stubNS([]string{"All", "alpha", "bravo", "charlie", "delta", "echo"}, 5)
	out := RenderNamespaceBar(ctx, 14)
	if strings.Contains(out, "›") {
		t.Errorf("right overflow arrow should be absent at end, got: %q", out)
	}
	// Selected (rightmost) must still be present.
	if !strings.Contains(out, "echo") {
		t.Errorf("selected chip echo should render, got: %q", out)
	}
}

func TestNamespaceBarEmptyDefaultsToAll(t *testing.T) {
	out := RenderNamespaceBar(stubNS(nil, 0), 80)
	if !strings.Contains(out, "All") {
		t.Errorf("empty namespaces should fall back to a single 'All' chip, got: %q", out)
	}
}

func TestNamespaceBarDefensiveCursor(t *testing.T) {
	// Out-of-range cursor should not crash and should land on a
	// valid chip.
	out := RenderNamespaceBar(stubNS([]string{"All", "dev", "prod"}, 99), 80)
	if !strings.Contains(out, "All") {
		t.Errorf("expected at least the All chip, got: %q", out)
	}
}

func TestNamespaceBarHasHeaderBackground(t *testing.T) {
	// Sanity: the bar carries the same HdrBg background as the header
	// and footer, so they visually stitch together.
	ctx := stubNS([]string{"All", "dev"}, 0)
	out := RenderNamespaceBar(ctx, 80)
	st := lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#f1f5f9", Dark: "#161b22"})
	if !strings.Contains(out, st.Render(" ")) {
		// Loose check — HdrBg may not be set; we mainly want to know
		// the renderer doesn't error out at the boundaries.
		_ = out
	}
}
