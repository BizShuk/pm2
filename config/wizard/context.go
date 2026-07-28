package wizard

import "io"

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
