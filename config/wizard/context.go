// Package wizard is the question-answer + rendering core of the
// ecosystem configuration wizard. It owns nothing about Cobra — every
// interactive routine accepts a WizardContext that carries the input
// and output streams explicitly. That makes the package usable from
// CLI, TUI, daemon, or any future entry point, and trivially mockable
// in tests (a strings.Reader + bytes.Buffer is enough to drive the
// full flow without a TTY).
//
// The dependency graph is one-way:
//
//	cmd -> wizard -> process, config
//
// The wizard package MUST NOT import cmd/. The cmd/ shell is a thin
// Cobra wrapper that parses flags, decides TTY vs. pipe mode, then
// calls RunInteractive / RunInstall here.
package wizard

import "io"

// Format constants used by the renderer and exposed to callers that
// need to validate --format values before invoking RunInteractive.
const (
	FormatJS   = "js"
	FormatJSON = "json"
)

// DefaultVersion is the placeholder version string written to new
// AppConfigs that don't supply their own. Exposed so the cmd/ install
// subcommand (which builds AppConfigs outside the wizard) can stay
// in sync without hard-coding the same magic string twice.
const DefaultVersion = "-"

// WizardContext carries the I/O streams the wizard reads from and
// writes to. Every exported entry point in this package takes one as
// the first argument so the caller controls where prompts and
// previews go — the package never touches os.Stdin / os.Stdout
// directly.
//
// YesAll is exposed here rather than inside Options because it is a
// property of the input medium (e.g. piped non-interactive shell),
// not of any single command's flags.
type WizardContext struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
	YesAll bool
}