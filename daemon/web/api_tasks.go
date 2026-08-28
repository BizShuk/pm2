package web

import (
	"net/http"
	"strconv"

	"github.com/bizshuk/pm2/runhistory"
)

// maxQueryLimit caps how much history one request can pull, so a
// reachable-from-anywhere endpoint cannot be used to make the daemon
// read its whole journal on demand.
const maxQueryLimit = 500

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"service": "pm2", "ok": true})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, newDaemonView(s.backend.DaemonStatus()))
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	writeJSONCached(w, r, newTaskViews(s.backend.ListTasks()))
}

func (s *Server) handleTaskRuns(w http.ResponseWriter, r *http.Request) {
	records, err := s.history.RecentTasks(queryFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "read task history: "+err.Error())
		return
	}
	if records == nil {
		records = []runhistory.TaskRecord{}
	}
	writeJSON(w, http.StatusOK, records)
}

// queryFrom builds a history query from the URL. An unparsable limit
// falls back to the default rather than 400: a dashboard's own query
// string is not something a user typed, and refusing to render because
// of it would be worse than rendering the default page.
func queryFrom(r *http.Request) runhistory.Query {
	q := runhistory.Query{
		Name:   r.URL.Query().Get("name"),
		Status: runhistory.Status(r.URL.Query().Get("status")),
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			q.Limit = min(n, maxQueryLimit)
		}
	}
	return q
}
