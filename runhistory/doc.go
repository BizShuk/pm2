// Package runhistory is the durable record of what pm2 has actually run.
//
// It keeps two append-only JSONL journals under the pm2 state root:
//
//	<root>/tasks/runs/YYYY-MM-DD.jsonl        one line per finished task run
//	<root>/workflows/runs/YYYY-MM-DD.jsonl    one line per finished workflow run
//
// The invariant that shapes everything else: the journal holds finished
// runs; the daemon reports running ones. A JSONL line cannot be updated,
// so recording a run at its start would mean either a second line to
// fold against or a file that is not really append-only. Instead a run
// is written once, when it is over. The cost is explicit: a run in
// flight when the daemon dies is never indexed — its stage log files
// still exist, only the index line is lost.
//
// The two exceptions are fires that produce no run at all
// (EventCronSkip, EventLaunchFail). They are recorded because
// "nothing happened" and "nothing was scheduled" look identical
// otherwise.
//
// Dependencies: stdlib only. This package is a sibling of logfile, not a
// child of daemon, because it has three unrelated consumers — the daemon
// writes it, daemon/web reads it, and cmd/workflow reads it — and living
// under daemon/ would force cmd/ to import daemon.
package runhistory
