package web

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bizshuk/pm2/daemon/wfengine"
	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/runhistory"
)

// --- stubs -----------------------------------------------------------

type stubBackend struct {
	tasks     []process.ProcessInfo
	status    process.DaemonInfo
	workflows []WorkflowSummary
	active    []RunSummary
	triggerFn func(name, trigger string) (RunSummary, error)
	triggered []string
}

func (b *stubBackend) ListTasks() []process.ProcessInfo { return b.tasks }
func (b *stubBackend) DaemonStatus() process.DaemonInfo { return b.status }
func (b *stubBackend) ListWorkflows() []WorkflowSummary { return b.workflows }
func (b *stubBackend) ActiveRuns() []RunSummary         { return b.active }
func (b *stubBackend) TriggerWorkflow(name, trigger string) (RunSummary, error) {
	b.triggered = append(b.triggered, name)
	if b.triggerFn != nil {
		return b.triggerFn(name, trigger)
	}
	return RunSummary{RunID: "20260828T030012-a1b2c3", Workflow: name, Trigger: trigger}, nil
}

type stubHistory struct {
	tasks     []runhistory.TaskRecord
	workflows []runhistory.WorkflowRecord
	run       runhistory.WorkflowRecord
	runFound  bool
	logPath   string
}

func (h *stubHistory) RecentTasks(runhistory.Query) ([]runhistory.TaskRecord, error) {
	return h.tasks, nil
}
func (h *stubHistory) RecentWorkflows(runhistory.Query) ([]runhistory.WorkflowRecord, error) {
	return h.workflows, nil
}
func (h *stubHistory) WorkflowRun(string) (runhistory.WorkflowRecord, bool, error) {
	return h.run, h.runFound, nil
}
func (h *stubHistory) StageLogPath(_, _, _ string) string { return h.logPath }

func newTestServer(b Backend, h HistoryReader) *Server {
	if b == nil {
		b = &stubBackend{}
	}
	if h == nil {
		h = &stubHistory{}
	}
	return New(b, h, DefaultHost, DefaultPort)
}

func do(t *testing.T, s *Server, method, path string, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

// --- the one that matters most ---------------------------------------

// TestTaskViewOmitsEnv is the most important test in this package.
//
// process.ProcessInfo embeds AppConfig, which carries Env and BaseEnv —
// the latter a snapshot of the user's interactive shell environment.
// Marshalling one on a port reachable from other machines would publish
// every exported token in the operator's shell profile.
func TestTaskViewOmitsEnv(t *testing.T) {
	backend := &stubBackend{tasks: []process.ProcessInfo{{
		AppConfig: process.AppConfig{
			Namespace: "default",
			Name:      "api",
			Script:    "./bin/api",
			Env:       map[string]string{"DATABASE_PASSWORD": "hunter2"},
			BaseEnv:   []string{"AWS_SECRET_ACCESS_KEY=super-secret-value", "PATH=/usr/bin"},
		},
		ID: 1, PID: 42, Status: process.StatusOnline,
	}}}

	body := do(t, newTestServer(backend, nil), http.MethodGet, "/api/tasks", "", nil).Body.String()

	for _, forbidden := range []string{
		"DATABASE_PASSWORD", "hunter2",
		"AWS_SECRET_ACCESS_KEY", "super-secret-value",
		"base_env", `"env"`,
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("response leaked %q:\n%s", forbidden, body)
		}
	}
	// It still has to be useful.
	for _, want := range []string{`"name":"api"`, `"pid":42`, `"status":"online"`} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q:\n%s", want, body)
		}
	}
}

// --- binding ---------------------------------------------------------

func TestDefaultBindIsAllInterfaces(t *testing.T) {
	s := New(&stubBackend{}, &stubHistory{}, DefaultHost, 0)
	s.port = freePort(t)
	if err := s.Bind(); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	defer s.listener.Close()

	addr, ok := s.listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("want a TCP listener, got %T", s.listener.Addr())
	}
	if !addr.IP.IsUnspecified() {
		t.Errorf("default bind should be every interface, got %s", addr.IP)
	}
}

// TestWebHostOverrideBindsLoopback pins the escape hatch: a machine that
// wants the dashboard closed off must be able to get there with a flag.
func TestWebHostOverrideBindsLoopback(t *testing.T) {
	s := New(&stubBackend{}, &stubHistory{}, "127.0.0.1", freePort(t))
	if err := s.Bind(); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	defer s.listener.Close()

	addr := s.listener.Addr().(*net.TCPAddr)
	if !addr.IP.IsLoopback() {
		t.Errorf("--web-host 127.0.0.1 must bind loopback, got %s", addr.IP)
	}
}

func TestURLRewritesWildcardForABrowser(t *testing.T) {
	s := New(&stubBackend{}, &stubHistory{}, DefaultHost, freePort(t))
	if err := s.Bind(); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	defer s.listener.Close()

	if !strings.HasPrefix(s.URL(), "http://localhost:") {
		t.Errorf("0.0.0.0 is an address to listen on, not to visit; got %s", s.URL())
	}
}

// --- webhook ---------------------------------------------------------

func TestWebhookAccepted(t *testing.T) {
	backend := &stubBackend{}
	w := do(t, newTestServer(backend, nil), http.MethodPost, "/api/webhooks/nightly",
		`{"params":{"DATE":"2026-08-28"}}`, map[string]string{"Content-Type": "application/json"})

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", w.Code, w.Body)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "/api/workflows/runs/") {
		t.Errorf("want a Location pointing at the run, got %q", loc)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["run_id"] == "" || body["workflow"] != "nightly" {
		t.Errorf("unexpected body: %v", body)
	}
	// Whatever a caller sent is its own business, not this endpoint's to
	// republish — and the params are exactly where a secret would be.
	if strings.Contains(w.Body.String(), "2026-08-28") {
		t.Errorf("202 must not echo params back: %s", w.Body)
	}
}

func TestWebhookRequiresJSONContentType(t *testing.T) {
	w := do(t, newTestServer(nil, nil), http.MethodPost, "/api/webhooks/nightly",
		`{}`, map[string]string{"Content-Type": "text/plain"})
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("want 415, got %d: %s", w.Code, w.Body)
	}
}

func TestWebhookRejectsBadJSON(t *testing.T) {
	w := do(t, newTestServer(nil, nil), http.MethodPost, "/api/webhooks/nightly",
		`{"params": [1,2,3]}`, map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", w.Code, w.Body)
	}
}

func TestWebhookRejectsUnknownField(t *testing.T) {
	w := do(t, newTestServer(nil, nil), http.MethodPost, "/api/webhooks/nightly",
		`{"prams":{"a":"b"}}`, map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("a misspelled field must not be silently ignored; got %d", w.Code)
	}
}

func TestWebhookRejectsJunkParams(t *testing.T) {
	w := do(t, newTestServer(nil, nil), http.MethodPost, "/api/webhooks/nightly",
		`{"params":{"bad name":"x"}}`, map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for an invalid param name, got %d", w.Code)
	}
}

// TestWebhookConflictWhenRunning: the caller did not start what it asked
// for, and 202 with somebody else's run id would be a lie it acts on.
func TestWebhookConflictWhenRunning(t *testing.T) {
	backend := &stubBackend{triggerFn: func(string, string) (RunSummary, error) {
		return RunSummary{}, wfengine.ErrRunInProgress
	}}
	w := do(t, newTestServer(backend, nil), http.MethodPost, "/api/webhooks/nightly",
		`{}`, map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusConflict {
		t.Errorf("want 409, got %d: %s", w.Code, w.Body)
	}
}

// TestWebhookUnknownWorkflowDoesNotEnumerate: an open endpoint must not
// double as a directory of what this machine runs.
func TestWebhookUnknownWorkflowDoesNotEnumerate(t *testing.T) {
	backend := &stubBackend{triggerFn: func(string, string) (RunSummary, error) {
		return RunSummary{}, errors.New("unknown workflow: nosuch (known: ci:payroll, ci:backups)")
	}}
	// The engine's real error is wrapped; the handler matches on the
	// sentinel, so use it here.
	backend.triggerFn = func(string, string) (RunSummary, error) {
		return RunSummary{}, wfengine.ErrUnknownWorkflow
	}

	w := do(t, newTestServer(backend, nil), http.MethodPost, "/api/webhooks/nosuch",
		`{}`, map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
	for _, leak := range []string{"payroll", "backups", "known:"} {
		if strings.Contains(w.Body.String(), leak) {
			t.Errorf("404 must not enumerate workflows, leaked %q: %s", leak, w.Body)
		}
	}
}

func TestWebhookRateLimit(t *testing.T) {
	s := newTestServer(nil, nil)
	for i := 0; i < rateBurst; i++ {
		w := do(t, s, http.MethodPost, "/api/webhooks/hot", `{}`, map[string]string{"Content-Type": "application/json"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("request %d: want 202, got %d", i+1, w.Code)
		}
	}
	w := do(t, s, http.MethodPost, "/api/webhooks/hot", `{}`, map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("request %d: want 429, got %d", rateBurst+1, w.Code)
	}

	// A different workflow has its own budget.
	other := do(t, s, http.MethodPost, "/api/webhooks/cool", `{}`, map[string]string{"Content-Type": "application/json"})
	if other.Code != http.StatusAccepted {
		t.Errorf("the limit is per workflow; got %d", other.Code)
	}
}

func TestRateLimiterForgetsOldAttempts(t *testing.T) {
	l := newRateLimiter()
	now := time.Now()
	l.now = func() time.Time { return now }

	for i := 0; i < rateBurst; i++ {
		if !l.allow("x") {
			t.Fatalf("attempt %d should be within budget", i+1)
		}
	}
	if l.allow("x") {
		t.Fatal("budget should be exhausted")
	}

	now = now.Add(rateWindow + time.Second)
	if !l.allow("x") {
		t.Error("the window should have rolled")
	}
}

// --- routing ---------------------------------------------------------

func TestRoutingTable(t *testing.T) {
	s := newTestServer(nil, &stubHistory{runFound: false})

	tests := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/", http.StatusOK},
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodGet, "/api/status", http.StatusOK},
		{http.MethodGet, "/api/tasks", http.StatusOK},
		{http.MethodGet, "/api/tasks/runs", http.StatusOK},
		{http.MethodGet, "/api/workflows", http.StatusOK},
		{http.MethodGet, "/api/workflows/runs", http.StatusOK},
		{http.MethodGet, "/api/workflows/runs/abc", http.StatusNotFound},
		{http.MethodGet, "/nope", http.StatusNotFound},
		{http.MethodGet, "/api/webhooks/x", http.StatusMethodNotAllowed},
		// No task-mutating route exists, deliberately.
		{http.MethodPost, "/api/tasks", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/tasks/nightly/restart", http.StatusNotFound},
	}
	for _, tt := range tests {
		w := do(t, s, tt.method, tt.path, "", nil)
		if w.Code != tt.want {
			t.Errorf("%s %s: want %d, got %d", tt.method, tt.path, tt.want, w.Code)
		}
	}
}

func TestTasksETagNotModified(t *testing.T) {
	s := newTestServer(&stubBackend{tasks: []process.ProcessInfo{{
		AppConfig: process.AppConfig{Name: "api"}, ID: 1,
	}}}, nil)

	first := do(t, s, http.MethodGet, "/api/tasks", "", nil)
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("want an ETag on the polled endpoint")
	}

	second := do(t, s, http.MethodGet, "/api/tasks", "", map[string]string{"If-None-Match": etag})
	if second.Code != http.StatusNotModified {
		t.Errorf("want 304 for unchanged state, got %d", second.Code)
	}
}

func TestIndexServedWithCSP(t *testing.T) {
	w := do(t, newTestServer(nil, nil), http.MethodGet, "/", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "connect-src 'self'") {
		t.Errorf("connect-src 'self' is what stops the page talking to anyone else; got %q", csp)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
	if !strings.Contains(w.Body.String(), "<title>pm2</title>") {
		t.Error("index does not look like the dashboard")
	}
}

// --- stage log -------------------------------------------------------

// TestStageLogRejectsUnknownStage: the path is built from the journal's
// own record, never from the URL, so a stage name cannot be used to
// walk out of the log directory.
func TestStageLogRejectsUnknownStage(t *testing.T) {
	history := &stubHistory{
		runFound: true,
		run: runhistory.WorkflowRecord{
			RunID: "abc", Workflow: "ci:nightly",
			Stages: []runhistory.StageRecord{{Name: "fetch"}},
		},
	}
	w := do(t, newTestServer(nil, history), http.MethodGet,
		"/api/workflows/runs/abc/logs/..%2f..%2fdump.json", "", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 for a stage the run never declared, got %d", w.Code)
	}
}

// --- helpers ---------------------------------------------------------

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
