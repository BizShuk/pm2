// Package views contains the stateless renderers that turn a ViewContext
// into the strings displayed by `pm2 monit`.
//
// Views in this package are pure functions: they consume a ViewContext
// and return a string. They never mutate state and never reach into the
// controller (tui.Model). All styling is sourced from the tui/theme
// palette, so swapping the colour scheme is a single-file change.
package views

import (
	"time"

	"github.com/bizshuk/pm2/process"
)

// ViewContext encapsulates every piece of state that the view layer
// needs to draw the next frame. The controller builds this from its
// internal model and passes it to RenderLayout; views never touch the
// controller directly.
//
// SortBy is passed as a string ("name" / "namespace" / "cpu" /
// "memory" / "status") rather than as a typed enum so this package
// stays decoupled from tui.SortField. The controller converts once per
// frame.
type ViewContext struct {
	Width      int                   // total terminal width
	Height     int                   // total terminal height
	Selected   int                   // index of the highlighted row
	Procs      []process.ProcessInfo // current process snapshot (already filtered by namespace)
	Namespaces []string              // ["All"] + unique sorted namespaces; index 0 == All
	NsCursor   int                   // index into Namespaces for the active filter chip
	Logs       []string              // tail of the selected process's log
	Updated    time.Time             // last successful refresh
	HostCPU    float64               // host CPU % (latest sample)
	HostMem    float64               // host memory % (latest sample)
	SortBy     string                // active sort label for the footer
	Err        error                 // last refresh / RPC error
	Notice     string                // transient action failure notice
	Detail     bool                  // two-pane (true) vs wide-table (false)
	LogFocus   bool                  // hide detail block; show only log tail at full height
}

// Detail rows — kept here so layout / detail / logs can agree without
// reaching back into the tui package. Detail renders 17 rows plus 1
// header plus 1 blank divider plus 1 log header = 20 rows total before
// the log panel.
const detailRows = 17