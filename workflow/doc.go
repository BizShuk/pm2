// Package workflow is the linear-orchestration domain: what a workflow
// is, how its stages are validated, and where its artifacts live.
//
// A workflow wraps a sequence of stages and runs them in order, stopping
// at the first failure. Two invariants shape the whole design:
//
//   - A stage runs exactly once. Success is exit code 0 and nothing
//     else. None of pm2's supervision behaviour — auto-restart, cron
//     restart, file watching, instance counts — applies to a stage,
//     because a stage is an execution, not a registration.
//
//   - The engine keeps no resumable state machine. A run executes
//     start to finish in memory; a daemon restart does not continue an
//     interrupted run and does not leave one claiming to be running.
//
// Like sysmon and logfile, this package holds no daemon reference, does
// no rendering, and speaks no RPC. The runtime engine lives in
// daemon/wfengine and the durable run records live in runhistory.
package workflow
