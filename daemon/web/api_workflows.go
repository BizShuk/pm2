package web

import (
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/bizshuk/pm2/runhistory"
)

// defaultLogTail bounds how much of a stage log one request returns.
const defaultLogTail = 500

func (s *Server) handleWorkflows(w http.ResponseWriter, _ *http.Request) {
	list := s.backend.ListWorkflows()
	if list == nil {
		list = []WorkflowSummary{}
	}
	writeJSON(w, http.StatusOK, list)
}

// handleWorkflowRuns puts the runs in flight ahead of the journal.
//
// The journal only holds finished runs, so the daemon is the only source
// for a run happening right now; concatenating the two is what makes the
// listing describe the present rather than the recent past.
func (s *Server) handleWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	query := queryFrom(r)

	records, err := s.history.RecentWorkflows(query)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "read workflow history: "+err.Error())
		return
	}

	type runRow struct {
		runhistory.WorkflowRecord
		Running bool `json:"running"`
	}
	rows := make([]runRow, 0, len(records)+4)
	for _, active := range s.backend.ActiveRuns() {
		if query.Name != "" && active.Workflow != query.Name {
			continue
		}
		category, name, _ := strings.Cut(active.Workflow, ":")
		rows = append(rows, runRow{
			WorkflowRecord: runhistory.WorkflowRecord{
				RunID: active.RunID, Kind: runhistory.KindWorkflow,
				Workflow: active.Workflow, Category: category, Name: name,
				Trigger: active.Trigger, StartedAt: active.StartedAt,
			},
			Running: true,
		})
	}
	for _, rec := range records {
		rows = append(rows, runRow{WorkflowRecord: rec})
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) handleWorkflowRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("runID")
	rec, ok, err := s.history.WorkflowRun(runID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "read workflow history: "+err.Error())
		return
	}
	if !ok {
		// A run in flight is not journaled until it finishes, so say so
		// rather than letting the dashboard read this as "never existed".
		writeErr(w, http.StatusNotFound, "no finished run with that id")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handleStageLog(w http.ResponseWriter, r *http.Request) {
	runID, stage := r.PathValue("runID"), r.PathValue("stage")

	rec, ok, err := s.history.WorkflowRun(runID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "read workflow history: "+err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "no finished run with that id")
		return
	}

	// The path is built from the journal's own record, never from the
	// request: a stage name straight off the URL is a traversal waiting
	// to happen, and only a stage this run actually declared can have a log.
	var known bool
	for _, st := range rec.Stages {
		if st.Name == stage {
			known = true
			break
		}
	}
	if !known {
		writeErr(w, http.StatusNotFound, "run has no stage with that name")
		return
	}

	data, err := readTail(s.history.StageLogPath(rec.Workflow, rec.RunID, stage), logTailFrom(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, "stage log unavailable")
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

func logTailFrom(r *http.Request) int {
	if raw := r.URL.Query().Get("tail"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return min(n, 5000)
		}
	}
	return defaultLogTail
}

// readTail returns the last n lines of a file.
func readTail(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, 4<<20))
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}
