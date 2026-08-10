package daemon

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/bizshuk/pm2/logfile"
)

// daemonLogName is the daemon's own log, a sibling of the managed
// task logs and rotated by the same rules.
const daemonLogName = "daemon.log"

// installLog routes the daemon's slog output to a rotating
// logfile.Writer under homeDir and returns a close func.
//
// Before this, the daemon logged to the stderr its supervisor handed
// it: an append-only file with no rotation and no owner, which grew
// without bound for as long as the daemon lived. Managed tasks already
// had daily rotation through logfile.Writer; the daemon supervising
// them was the one process still writing an unbounded file.
//
// The handler drops slog's own time attribute because logfile.Writer
// stamps every logical line itself — and it is that stamp the rotation
// logic reads back to decide which leading block belongs to an earlier
// day. Two timestamps per line would make the second one noise and the
// first one load-bearing, which is a trap for whoever edits this next.
//
// A writer that cannot be opened is not fatal: a daemon that refuses to
// start because of its log file is strictly worse than one that logs to
// the inherited stderr. In that case the default handler stays in place.
func installLog(homeDir string) (func(), error) {
	writer, err := logfile.Open(filepath.Join(homeDir, daemonLogName))
	if err != nil {
		return func() {}, fmt.Errorf("open daemon log: %w", err)
	}

	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return attr
		},
	})
	slog.SetDefault(slog.New(handler))

	return func() { _ = writer.Close() }, nil
}

// installLogOrWarn applies installLog and reports a failure on the
// inherited stderr, where a log-file problem is the one message that
// cannot be written to the log file.
func installLogOrWarn(homeDir string) func() {
	closeLog, err := installLog(homeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: daemon log rotation disabled: %v\n", err)
	}
	return closeLog
}
