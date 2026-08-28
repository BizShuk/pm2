package runhistory

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// runIDLayout is the timestamp half of a run ID. The date prefix is
// load-bearing, not decoration: WorkflowRun parses it to open exactly
// one day file instead of walking the whole journal directory. Do not
// "clean this up" into a UUID.
const runIDLayout = "20060102T150405"

// NewRunID returns a sortable, filename-safe, collision-resistant run
// identifier of the form 20260828T030012-a1b2c3.
func NewRunID(t time.Time) string {
	var b [3]byte
	// crypto/rand.Read never returns an error as of Go 1.24.
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s-%s", t.Format(runIDLayout), hex.EncodeToString(b[:]))
}

// dayOfRunID recovers the journal day a run ID belongs to. ok is false
// for an identifier that did not come from NewRunID, in which case the
// caller must fall back to scanning every day file.
func dayOfRunID(runID string) (day string, ok bool) {
	if len(runID) < len(runIDLayout) {
		return "", false
	}
	t, err := time.ParseInLocation(runIDLayout, runID[:len(runIDLayout)], time.Local)
	if err != nil {
		return "", false
	}
	return t.Format(dayLayout), true
}
