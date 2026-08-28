// Package web serves pm2's HTTP interface: a browser dashboard over the
// tasks and workflows the daemon manages, and a webhook that triggers a
// workflow run.
//
// # This is a deliberately unauthenticated public endpoint
//
// The server binds 0.0.0.0 by default and checks no credential. That is
// an explicit product decision, not an oversight, and it overrides the
// workspace rule that an unauthenticated interface stays on loopback.
// What it means concretely: anyone who can reach this port can trigger a
// workflow, and a workflow stage runs a shell command. Treat reachability
// to this port as equivalent to shell access on this machine.
//
// Two consequences are load-bearing:
//
//   - The daemon logs a warning naming the address and the absence of
//     authentication every time it starts. Operators must be able to see
//     the exposure without reading this file.
//   - There is no task-mutating route. The webhook carries the risk the
//     product asked for; nothing widens it to restarting or deleting the
//     user's processes.
//
// # Never serialise process.ProcessInfo
//
// ProcessInfo embeds process.AppConfig, which carries Env and — worse —
// BaseEnv, a snapshot of the user's interactive shell environment taken
// by the CLI at apply time. Marshalling one would publish every exported
// token in the operator's shell profile. Every handler projects into the
// view types in view.go, whose fields are listed one by one, and
// TestTaskViewOmitsEnv exists to keep it that way.
//
// # Imports
//
// This package never imports daemon: everything it needs arrives through
// the Backend interface declared here, the same guard daemon/network
// applies with network.Manager. It does not import the workflow package
// either — it declares its own view types — so it compiles and is
// httptest-testable on its own, and the daemon-side adapter does the
// conversion.
//
// # Relationship to the event-stream plan
//
// plans/2026-07-23-pm2-event-stream.md rules out a built-in public HTTP
// surface. This package overrides that clause and only that clause:
// there is still no OAuth, no TLS, no credential store, and no webhook
// registry — a workflow definition *is* its registration, declared in the
// ecosystem file beside everything else. The two planes remain different
// in kind: the event socket is a push plane for programs, this is a pull
// plane for one person with a browser.
package web
