package web

import "net/http"

// Handler builds the whole routing table. Go 1.22 method-and-wildcard
// patterns cover the entire surface, so this server needs no router
// dependency — the point of the Dependencies table staying unchanged.
//
// There is deliberately no task-mutating route. The webhook carries the
// risk the product asked for; adding restart or delete would let any
// reachable host stop the user's services, which nobody asked for.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// {$} matches the root exactly, so every unrouted path 404s instead
	// of being swallowed by a catch-all that serves the dashboard.
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /healthz", s.handleHealth)

	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/tasks", s.handleTasks)
	mux.HandleFunc("GET /api/tasks/runs", s.handleTaskRuns)

	mux.HandleFunc("GET /api/workflows", s.handleWorkflows)
	mux.HandleFunc("GET /api/workflows/runs", s.handleWorkflowRuns)
	mux.HandleFunc("GET /api/workflows/runs/{runID}", s.handleWorkflowRun)
	mux.HandleFunc("GET /api/workflows/runs/{runID}/logs/{stage}", s.handleStageLog)

	mux.HandleFunc("POST /api/webhooks/{workflow}", s.handleWebhook)

	// Every route goes through the guard, not just the webhook: the read
	// endpoints expose the task table, its configuration, and the run
	// history, which a page on another origin has no business pulling.
	return s.guard(mux)
}
