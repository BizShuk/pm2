// Package gpuagent is the privileged half of pm2's GPU reading: a small
// root process that drives `powermetrics` and publishes each sample to a
// world-readable file.
//
// It exists so that nothing else has to be root. macOS exposes GPU
// residency and power through `powermetrics` alone, and that tool
// refuses to run as a normal user. Running the pm2 daemon as root to
// reach it would make every managed task root as well, because
// executor.BuildCommand passes no Credential, and would leave the
// socket, dump.json and every application log owned by root. One
// short-lived contract — a JSON file at sysmon.DefaultGPUExportPath —
// keeps the elevated surface to this package and this package alone.
//
// Boundaries:
//
//   - gpuagent writes; sysmon reads. Neither calls the other, and the
//     file is the whole interface between them.
//   - gpuagent never renders and never speaks RPC, matching the rest of
//     the sysmon domain.
//   - Publishing is atomic (write to a sibling temp file, then rename),
//     so a reader is never handed a half-written sample.
package gpuagent
