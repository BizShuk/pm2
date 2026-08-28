// Package wfengine executes workflows: it holds the registered
// definitions, arms their schedules, and runs their stages in order.
//
// # Stages bypass the supervised path
//
// A stage is spawned with executor.BuildCommand and waited on directly,
// never through executor.Start and the process registry. Restart policy
// is part of the reason — a stage that legitimately exits 1 would
// otherwise be resurrected up to MaxRestarts times — but the decisive
// reason is identity. A `task:` stage runs an AppConfig whose registry
// key is the key of a task that is already registered, so going through
// StartApp would stop and replace the user's live long-running service
// and then point its registry entry at a short-lived child. A stage is
// an execution, not a registration.
//
// # Single-flight is the cycle guard that actually holds
//
// Three layers stop a workflow from calling itself forever, and they are
// not equally load-bearing:
//
//   - workflow.CheckAcyclic rejects every declared cycle at registration.
//   - The ancestry chain on a run rejects a nested call to a workflow
//     already on the path, with a much better error message.
//   - Per-workflow single flight rejects a second run of a workflow that
//     is already running.
//
// Only the third one covers a stage whose shell script re-triggers the
// workflow — through `pm2 workflow run`, or through the webhook. That
// arrives as a brand-new request with an empty ancestry chain, so the
// static and ancestry checks cannot see it at all. Do not simplify
// single flight away as "merely an overlap nicety"; on a public,
// unauthenticated webhook it is also the only thing bounding how much
// work a remote caller can start.
//
// # Imports
//
// This package never imports daemon. Everything it needs from the
// process registry arrives through the TaskLookup interface, mirroring
// how daemon/network depends only on network.Manager.
package wfengine
