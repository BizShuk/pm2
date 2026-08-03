// Package sysmon observes what the machine is doing right now: CPU,
// memory, load, network throughput, block-device I/O, filesystem usage,
// the OS process table, and listening TCP ports.
//
// It is the single owner of host-level measurement in pm2. The OS is read
// through platform samplers (darwin / linux / fallback) chosen at
// construction time by runtime.GOOS rather than by build tags, so every
// parser compiles — and can be unit tested — on every platform.
//
// Boundaries:
//
//   - sysmon never renders. Callers own all formatting; the package
//     returns numbers and plain strings only.
//   - sysmon imports process/ solely to join managed applications with
//     their OS detail in Snapshot. It must not import daemon, cmd, or tui.
//   - Cumulative OS counters (network bytes, disk sectors) are converted
//     to per-second rates by the Collector, which therefore holds state
//     between samples and is safe for concurrent use.
package sysmon
