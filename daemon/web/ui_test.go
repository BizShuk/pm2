package web

import (
	"strings"
	"testing"

	"github.com/bizshuk/pm2/tui/theme"
)

// TestUIPaletteMatchesTheme keeps the dashboard and the TUI from
// disagreeing about what "errored" looks like.
//
// The page must stay a static asset, so the hex values are written into
// its CSS rather than templated in. This test is what makes that
// duplication safe — tui/views/width_test.go is the same idea applied to
// two width engines that had to agree.
func TestUIPaletteMatchesTheme(t *testing.T) {
	page := string(indexHTML)

	tokens := map[string]struct{ light, dark string }{
		"--online":  {theme.Online.Light, theme.Online.Dark},
		"--stopped": {theme.Stopped.Light, theme.Stopped.Dark},
		"--errored": {theme.Errored.Light, theme.Errored.Dark},
		"--warn":    {theme.Warn.Light, theme.Warn.Dark},
		"--muted":   {theme.Muted.Light, theme.Muted.Dark},
		"--text":    {theme.Text.Light, theme.Text.Dark},
	}

	for name, want := range tokens {
		for _, hex := range []string{want.light, want.dark} {
			if !strings.Contains(page, name+":"+hex) {
				t.Errorf("index.html is missing %s:%s — the dashboard and the TUI have drifted", name, hex)
			}
		}
	}
}

// TestUILoadsNothingRemote: a CDN script would break the page on an
// offline host and would tell a third party every time someone opened
// their own process dashboard.
func TestUILoadsNothingRemote(t *testing.T) {
	page := string(indexHTML)
	for _, remote := range []string{"http://", "https://", "//cdn", "integrity="} {
		if strings.Contains(page, remote) {
			t.Errorf("index.html references something remote (%q); it must be self-contained", remote)
		}
	}
}

// TestUIWarnsAboutTheOpenEndpoint: the exposure has to be visible to the
// person looking at the page, not only to whoever reads the source.
func TestUIWarnsAboutTheOpenEndpoint(t *testing.T) {
	if !strings.Contains(string(indexHTML), "without authentication") {
		t.Error("the dashboard must state that it is unauthenticated")
	}
}
