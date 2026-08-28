// Package web serves pm2's HTTP interface: a browser dashboard over the
// tasks and workflows the daemon manages, and a webhook that triggers a
// workflow run.
//
// # An admin console on the LAN, with no authentication
//
// The server binds 0.0.0.0 on a port from the internal segment
// (8500-8599) and checks no credential. Concretely: it opens from any
// machine on the local network, and there is deliberately no tunnel and
// no internet exposure — the LAN is the boundary.
//
// That combination is an explicit product decision and it deviates from
// the workspace port rule twice, in ways worth naming rather than
// hiding: the rule reads "LAN reachable -> public segment", and
// "internal -> bind 127.0.0.1". This service is numbered internal
// because it is an admin console, and bound LAN-wide because it is meant
// to be opened from a phone or a second machine.
//
// Three consequences are load-bearing:
//
//   - Anyone on the network who can reach this port can trigger a
//     workflow, and a workflow stage runs a shell command. Treat
//     reachability as equivalent to shell access on this machine.
//   - The daemon logs a warning naming the address and the absence of
//     authentication every time it starts. Operators must be able to see
//     the exposure without reading this file.
//   - There is no task-mutating route. The webhook carries the risk the
//     product asked for; nothing widens it to restarting or deleting the
//     user's processes.
//
// The bind address is therefore not the security boundary, and guard.go
// is not decoration: a page on another site, opened by anyone on this
// network, can POST here from their own browser. The same-origin check
// is what stops that, and it costs a real client nothing.
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
// surface. This package overrides that clause and only that clause —
// and only as far as the LAN:
// there is still no OAuth, no TLS, no credential store, and no webhook
// registry — a workflow definition *is* its registration, declared in the
// ecosystem file beside everything else. The two planes remain different
// in kind: the event socket is a push plane for programs, this is a pull
// plane for one person with a browser.
package web
