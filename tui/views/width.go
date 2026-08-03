package views

import "github.com/mattn/go-runewidth"

// screen is how this package measures text, and it exists because two
// measurement engines meet here and must agree.
//
// lipgloss does the actual padding and truncation of every panel, and it
// treats Unicode "Ambiguous width" characters — ● ○ │ ↑ → … █ — as one
// column, always. go-runewidth, which backs Crop and CropRight, reads
// LC_CTYPE at startup instead and reports those same characters as *two*
// columns under an East Asian locale.
//
// Under LC_CTYPE=zh_TW.UTF-8 that disagreement is not cosmetic. A status
// dot silently costs an extra column, a list row overflows its pane by
// one, lipgloss wraps it onto a second line, and the pane shows half the
// entries it should; the same locale truncated the log browser's key
// legend mid-word. Verified on Ubuntu 26.04 with LC_CTYPE=zh_TW.UTF-8,
// where runewidth reported ● as 2 and lipgloss as 1.
//
// Holding our own Condition rather than mutating runewidth's package
// globals keeps the choice local to the renderer: nothing else in the
// process has its text measurement changed out from under it.
//
// Genuinely wide characters are unaffected. CJK text and emoji are
// classified Wide, not Ambiguous, and still measure two columns — a task
// named "資料同步" crops correctly either way.
var screen = &runewidth.Condition{EastAsianWidth: false, StrictEmojiNeutral: true}
