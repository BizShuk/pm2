package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/bizshuk/pm2/daemon/wfengine"
	"github.com/bizshuk/pm2/runhistory"
)

// maxWebhookBody bounds the request body.
const maxWebhookBody = 64 << 10

// webhookRequest is the accepted body. Params is accepted and validated
// but not yet forwarded to the engine — see the note in handleWebhook.
type webhookRequest struct {
	Params map[string]string `json:"params,omitempty"`
}

// handleWebhook is the only route that changes anything.
//
// There is no credential check: the product asked for an open endpoint,
// and the package doc records what that means. The two checks that do
// run are not authentication in disguise — one is content negotiation,
// the other is an accident guard — and both are transparent to a real
// client, because curl, CI, and scripts all send a Content-Type and none
// of them retries ten times a minute.
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("workflow")

	// Require JSON. This is content negotiation first, but it also means
	// a cross-origin POST from a page in someone's browser must clear a
	// CORS preflight, which we never answer.
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		writeErr(w, http.StatusUnsupportedMediaType, "expected Content-Type: application/json")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "request body too large or unreadable")
		return
	}
	if len(strings.TrimSpace(string(body))) > 0 {
		var req webhookRequest
		dec := json.NewDecoder(strings.NewReader(string(body)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}
		if err := validateParams(req.Params); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if !s.limiter.allow(name) {
		writeErr(w, http.StatusTooManyRequests, "too many triggers for this workflow; try again shortly")
		return
	}

	run, err := s.backend.TriggerWorkflow(name, runhistory.TriggerWebhook)
	switch {
	case err == nil:
	case errors.Is(err, wfengine.ErrRunInProgress):
		// 409, not 202 with the in-flight id: the caller did not start
		// what it asked for, and saying otherwise would be a lie it acts on.
		writeErr(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, wfengine.ErrUnknownWorkflow):
		// The error does not list the known workflows. An open endpoint
		// must not double as a directory of what this machine runs.
		writeErr(w, http.StatusNotFound, "unknown workflow: "+name)
		return
	case errors.Is(err, wfengine.ErrTooManyRuns):
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	default:
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// 202, not 200: a workflow can run for an hour, and holding the
	// connection open would time out every ordinary HTTP client. The
	// caller polls the run instead. The params are not echoed back —
	// whatever a caller sent is its own business, not this endpoint's
	// to republish.
	w.Header().Set("Location", "/api/workflows/runs/"+run.RunID)
	writeJSON(w, http.StatusAccepted, map[string]string{
		"run_id":   run.RunID,
		"workflow": run.Workflow,
		"status":   "queued",
	})
}

// validateParams rejects junk at the edge. These values travel toward a
// shell, so bounding them here is the web layer's job; quoting them is
// the engine's.
func validateParams(params map[string]string) error {
	if len(params) > 32 {
		return errors.New("at most 32 params are accepted")
	}
	for k, v := range params {
		if !isParamName(k) {
			return errors.New("invalid param name: " + k)
		}
		if len(v) > 4<<10 {
			return errors.New("param value too long: " + k)
		}
	}
	return nil
}

func isParamName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}
