package runhistory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.ParseInLocation("2006-01-02T15:04:05", s, time.UTC)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts
}

func code(n int) *int { return &n }

// TestTaskRecordSchema pins the on-disk contract. The journal is a
// format other tools read, so a field rename must break this test
// rather than silently break a consumer.
func TestTaskRecordSchema(t *testing.T) {
	rec := TaskRecord{
		TS:         mustTime(t, "2026-08-28T03:00:12"),
		Event:      EventRun,
		RunID:      "20260828T030012-a1b2c3",
		Namespace:  "default",
		Name:       "nightly-scan",
		ID:         3,
		Trigger:    TriggerCron,
		PID:        4242,
		StartedAt:  mustTime(t, "2026-08-28T03:00:00"),
		DurationMS: 12000,
		ExitCode:   code(3),
		Restarts:   1,
		Status:     StatusFailed,
	}
	rec.Kind = KindTask

	got, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"ts":"2026-08-28T03:00:12Z","kind":"task","event":"run",` +
		`"run_id":"20260828T030012-a1b2c3","ns":"default","name":"nightly-scan",` +
		`"id":3,"trigger":"cron","pid":4242,"started_at":"2026-08-28T03:00:00Z",` +
		`"duration_ms":12000,"exit_code":3,"restarts":1,"status":"failed"}`
	if string(got) != want {
		t.Errorf("task record shape drifted:\n want %s\n  got %s", want, got)
	}
}

func TestWorkflowRecordSchema(t *testing.T) {
	rec := WorkflowRecord{
		RunID:      "20260828T030012-a1b2c3",
		Kind:       KindWorkflow,
		Workflow:   "ci:nightly",
		Category:   "ci",
		Name:       "nightly",
		Trigger:    TriggerWebhook,
		StartedAt:  mustTime(t, "2026-08-28T03:00:00"),
		FinishedAt: mustTime(t, "2026-08-28T03:02:00"),
		DurationMS: 120000,
		Status:     StatusFailed,
		Params:     map[string]string{"DATE": "2026-08-28"},
		Stages: []StageRecord{
			{Name: "fetch", Kind: "script", Status: StatusSuccess, ExitCode: code(0)},
			{Name: "load", Kind: "task", Ref: "loader", Status: StatusFailed, ExitCode: code(2)},
		},
	}

	got, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"run_id":"20260828T030012-a1b2c3","kind":"workflow","workflow":"ci:nightly",` +
		`"category":"ci","name":"nightly","trigger":"webhook",` +
		`"started_at":"2026-08-28T03:00:00Z","finished_at":"2026-08-28T03:02:00Z",` +
		`"duration_ms":120000,"status":"failed","params":{"DATE":"2026-08-28"},` +
		`"stages":[{"name":"fetch","kind":"script","status":"success","exit_code":0},` +
		`{"name":"load","kind":"task","ref":"loader","status":"failed","exit_code":2}]}`
	if string(got) != want {
		t.Errorf("workflow record shape drifted:\n want %s\n  got %s", want, got)
	}
}

// TestExitCodeUnknownIsNotZero is the reason ExitCode is a pointer: a
// launch that never produced a child has no exit code, and writing 0
// there would report it as a success.
func TestExitCodeUnknownIsNotZero(t *testing.T) {
	line, err := json.Marshal(TaskRecord{Event: EventLaunchFail, Status: StatusFailed})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(line), `"exit_code":null`) {
		t.Errorf("unknown exit code must serialise as null, got %s", line)
	}
}

func TestAppendThenQueryRoundTrip(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	defer s.Close()

	day1 := mustTime(t, "2026-08-27T10:00:00")
	day2 := mustTime(t, "2026-08-28T10:00:00")

	for i, at := range []time.Time{day1, day1.Add(time.Minute), day2, day2.Add(time.Minute)} {
		err := s.AppendTask(TaskRecord{
			TS: at, Event: EventRun, RunID: NewRunID(at),
			Namespace: "default", Name: fmt.Sprintf("task-%d", i),
			Trigger: TriggerCron, ExitCode: code(0), Status: StatusSuccess,
		})
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	if _, err := os.Stat(filepath.Join(TasksDir(root), "2026-08-27.jsonl")); err != nil {
		t.Fatalf("day file for 2026-08-27 missing: %v", err)
	}

	got, err := s.RecentTasks(Query{})
	if err != nil {
		t.Fatalf("RecentTasks: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 records across two day files, got %d", len(got))
	}
	want := []string{"task-3", "task-2", "task-1", "task-0"}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("record %d: want %s (newest first), got %s", i, name, got[i].Name)
		}
	}

	if got, err = s.RecentTasks(Query{Name: "default:task-2"}); err != nil || len(got) != 1 {
		t.Fatalf("namespaced name filter: got %d records, err %v", len(got), err)
	}
	if got, err = s.RecentTasks(Query{Limit: 2}); err != nil || len(got) != 2 {
		t.Fatalf("limit: got %d records, err %v", len(got), err)
	}
}

func TestRecentTasksFiltersByStatus(t *testing.T) {
	s := NewStore(t.TempDir())
	defer s.Close()

	now := time.Now()
	for i, st := range []Status{StatusSuccess, StatusFailed, StatusSuccess} {
		if err := s.AppendTask(TaskRecord{
			TS: now.Add(time.Duration(i) * time.Second), Event: EventRun,
			RunID: NewRunID(now), Name: "api", Status: st,
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	got, err := s.RecentTasks(Query{Status: StatusFailed})
	if err != nil || len(got) != 1 {
		t.Fatalf("want 1 failed record, got %d (err %v)", len(got), err)
	}
}

// TestWorkflowRunOpensOneDayFile is why a run ID carries its date. If the
// lookup fell back to scanning, deleting every other day file would not
// change the result — so the other files are removed to prove it did not.
func TestWorkflowRunOpensOneDayFile(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)

	at := mustTime(t, "2026-08-28T03:00:12")
	runID := NewRunID(at)
	if err := s.AppendWorkflow(WorkflowRecord{
		RunID: runID, Workflow: "ci:nightly", Category: "ci", Name: "nightly",
		StartedAt: at, FinishedAt: at, Status: StatusSuccess,
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	s.Close()

	// Decoys on other days that a directory scan would have to walk.
	for _, day := range []string{"2026-08-26", "2026-08-27"} {
		path := filepath.Join(WorkflowsDir(root), day+".jsonl")
		if err := os.WriteFile(path, []byte("{\"run_id\":\"decoy\"}\n"), 0o600); err != nil {
			t.Fatalf("write decoy: %v", err)
		}
	}

	got, ok, err := s.WorkflowRun(runID)
	if err != nil || !ok {
		t.Fatalf("WorkflowRun: ok=%v err=%v", ok, err)
	}
	if got.Workflow != "ci:nightly" {
		t.Errorf("want ci:nightly, got %s", got.Workflow)
	}

	if _, ok, err = s.WorkflowRun("20260828T030012-nosuch"); ok || err != nil {
		t.Errorf("unknown run: want (false, nil), got (%v, %v)", ok, err)
	}
}

func TestMissingJournalIsEmptyNotError(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "never-created"))

	tasks, err := s.RecentTasks(Query{})
	if err != nil || len(tasks) != 0 {
		t.Errorf("tasks: want (empty, nil), got (%d, %v)", len(tasks), err)
	}
	flows, err := s.RecentWorkflows(Query{})
	if err != nil || len(flows) != 0 {
		t.Errorf("workflows: want (empty, nil), got (%d, %v)", len(flows), err)
	}
}

// TestCorruptLineSkipped covers the ordinary aftermath of a kill -9: the
// last line is half-written. It must cost that one record, not the file.
func TestCorruptLineSkipped(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)

	now := time.Now()
	for i := 0; i < 3; i++ {
		if err := s.AppendTask(TaskRecord{
			TS: now.Add(time.Duration(i) * time.Second), Event: EventRun,
			RunID: NewRunID(now), Name: fmt.Sprintf("t%d", i), Status: StatusSuccess,
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	s.Close()

	path := filepath.Join(TasksDir(root), dayOf(now))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, journalFileMode)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString(`{"ts":"2026-08-28T03:0`); err != nil {
		t.Fatalf("write torn line: %v", err)
	}
	f.Close()

	got, err := s.RecentTasks(Query{})
	if err != nil {
		t.Fatalf("RecentTasks: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("want the 3 intact records, got %d", len(got))
	}
}

func TestPruneKeepsWindow(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	dir := TasksDir(root)
	if err := os.MkdirAll(dir, journalDirMode); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	now := time.Now()
	days := map[string]bool{ // name -> should survive
		now.Format(dayLayout):                     true,
		now.AddDate(0, 0, -29).Format(dayLayout):  true,
		now.AddDate(0, 0, -31).Format(dayLayout):  false,
		now.AddDate(0, 0, -400).Format(dayLayout): false,
	}
	for day := range days {
		if err := os.WriteFile(filepath.Join(dir, day+".jsonl"), []byte("{}\n"), journalFileMode); err != nil {
			t.Fatalf("seed %s: %v", day, err)
		}
	}
	// Not a day file — must be left alone entirely.
	notes := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notes, []byte("keep me"), 0o600); err != nil {
		t.Fatalf("seed notes: %v", err)
	}

	if err := s.Prune(DefaultKeepDays); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	for day, survives := range days {
		_, err := os.Stat(filepath.Join(dir, day+".jsonl"))
		if survives && err != nil {
			t.Errorf("%s should have survived: %v", day, err)
		}
		if !survives && err == nil {
			t.Errorf("%s should have been pruned", day)
		}
	}
	if _, err := os.Stat(notes); err != nil {
		t.Errorf("a non-day file must never be pruned: %v", err)
	}
}

func TestJournalFileMode(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	defer s.Close()

	if err := s.AppendTask(TaskRecord{Event: EventRun, Name: "api", Status: StatusSuccess}); err != nil {
		t.Fatalf("append: %v", err)
	}

	info, err := os.Stat(filepath.Join(TasksDir(root), dayOf(time.Now())))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != journalFileMode {
		t.Errorf("journal holds webhook params; want mode %o, got %o", journalFileMode, got)
	}
}

func TestConcurrentAppendsProduceWholeLines(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	defer s.Close()

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_ = s.AppendTask(TaskRecord{
					Event: EventRun, RunID: NewRunID(time.Now()),
					Name: fmt.Sprintf("w%d-%d", w, i), Status: StatusSuccess,
				})
			}
		}(w)
	}
	wg.Wait()

	data, err := os.ReadFile(filepath.Join(TasksDir(root), dayOf(time.Now())))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 800 {
		t.Fatalf("want 800 lines, got %d", len(lines))
	}
	for i, line := range lines {
		var rec TaskRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %d is not whole: %v", i, err)
		}
	}
}

// TestOversizedWorkflowRecordIsTruncated keeps one record to one atomic
// write. Params are dropped first because they are caller-supplied and
// unbounded; the run's identity and outcome always survive.
func TestOversizedWorkflowRecordIsTruncated(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	defer s.Close()

	huge := map[string]string{}
	for i := 0; i < 200; i++ {
		huge[fmt.Sprintf("key%d", i)] = strings.Repeat("x", 100)
	}
	runID := NewRunID(time.Now())
	if err := s.AppendWorkflow(WorkflowRecord{
		RunID: runID, Workflow: "ci:big", Category: "ci", Name: "big",
		Status: StatusSuccess, Params: huge,
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(WorkflowsDir(root), dayOf(time.Now())))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(data) > maxRecordBytes {
		t.Errorf("record must stay under %d bytes, got %d", maxRecordBytes, len(data))
	}

	got, ok, err := s.WorkflowRun(runID)
	if err != nil || !ok {
		t.Fatalf("WorkflowRun: ok=%v err=%v", ok, err)
	}
	if !got.Truncated {
		t.Error("a truncated record must say so")
	}
	if got.Params != nil {
		t.Error("params should have been dropped")
	}
	if got.Workflow != "ci:big" || got.Status != StatusSuccess {
		t.Errorf("identity and outcome must survive truncation, got %+v", got)
	}
}

func TestStageLogNameIsPathSafe(t *testing.T) {
	got := StageLogName("ci:nightly", "20260828T030012-a1b2c3", "../../etc/passwd")
	if strings.Contains(got, "/") || strings.Contains(got, "..") {
		t.Errorf("stage log name must not escape its directory, got %q", got)
	}
	if !strings.Contains(got, "20260828T030012-a1b2c3") {
		t.Errorf("stage log name must carry the run ID, got %q", got)
	}
}
