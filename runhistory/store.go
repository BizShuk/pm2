package runhistory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// maxRecordBytes caps one journal line. A record is written with a
	// single Write call, so staying under this bound is what makes a
	// concurrent reader safe against a torn tail.
	maxRecordBytes = 4 << 10

	// DefaultKeepDays is how long a journal day file survives.
	DefaultKeepDays = 30

	journalDirMode  = 0o700
	journalFileMode = 0o600
)

// Store owns both journals. It is safe for concurrent use.
//
// File mode is 0600, not dump.json's 0644: no other process needs to
// read these, and the workflow journal stores caller-supplied webhook
// params, which is exactly where a secret would end up.
//
// There is no fsync. An fsync per record would mean a disk flush every
// minute forever for a per-minute cron task, and a journal entry is an
// observability artifact, not a transaction. O_APPEND plus a single
// write(2) already survives a process crash, which is the common case.
type Store struct {
	root string

	mu       sync.Mutex
	tasks    *journal
	workflow *journal
}

// journal is one append-only day-rotated file.
type journal struct {
	dir  string
	day  string
	file *os.File
}

// NewStore returns a Store rooted at the pm2 state directory
// (~/.config/pm2). Nothing is created until the first append.
func NewStore(root string) *Store {
	return &Store{
		root:     root,
		tasks:    &journal{dir: TasksDir(root)},
		workflow: &journal{dir: WorkflowsDir(root)},
	}
}

// TasksDir is where task run records live.
func TasksDir(root string) string { return filepath.Join(root, "tasks", "runs") }

// WorkflowsDir is where workflow run records live.
func WorkflowsDir(root string) string { return filepath.Join(root, "workflows", "runs") }

// WorkflowLogsDir is where a workflow run's per-stage output lives. It
// is deliberately outside tasks/logs so logfile.ListTasks — and so
// `pm2 logs monitor` — never offers these for deletion, and so its
// stem-grouping rule never tries to split a run ID into a rotation date.
func WorkflowLogsDir(root string) string { return filepath.Join(root, "workflows", "logs") }

// AppendTask writes one task record.
func (s *Store) AppendTask(r TaskRecord) error {
	r.Kind = KindTask
	if r.TS.IsZero() {
		r.TS = time.Now()
	}
	return s.append(s.tasks, r.TS, r)
}

// AppendWorkflow writes one workflow record, dropping params and
// per-stage detail if the whole record would exceed maxRecordBytes.
func (s *Store) AppendWorkflow(r WorkflowRecord) error {
	r.Kind = KindWorkflow
	if r.FinishedAt.IsZero() {
		r.FinishedAt = time.Now()
	}
	if line, err := json.Marshal(r); err == nil && len(line)+1 > maxRecordBytes {
		r = truncateWorkflow(r)
	}
	return s.append(s.workflow, r.FinishedAt, r)
}

// truncateWorkflow strips the unbounded parts of a record — the
// caller-supplied params and each stage's free-text error — so the line
// stays a single atomic write. The run's identity and outcome, which is
// what the history is for, always survive.
func truncateWorkflow(r WorkflowRecord) WorkflowRecord {
	r.Params = nil
	r.Error = truncateString(r.Error, 256)
	for i := range r.Stages {
		r.Stages[i].Error = truncateString(r.Stages[i].Error, 128)
	}
	r.Truncated = true
	return r
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func (s *Store) append(j *journal, at time.Time, rec any) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	rotated, err := j.ensureDay(at)
	if err != nil {
		return err
	}
	if _, err := j.file.Write(line); err != nil {
		return fmt.Errorf("append %s: %w", j.dir, err)
	}

	// Opening a new day file is the one moment a prune is both due and
	// free — once per day, with no ticker and no goroutine of its own.
	if rotated {
		_ = pruneDir(j.dir, DefaultKeepDays, at)
	}
	return nil
}

// ensureDay opens (or reopens) the file for the record's own timestamp,
// not for the time the file happened to be opened. A daemon running
// across midnight therefore rolls on its next record.
func (j *journal) ensureDay(at time.Time) (rotated bool, err error) {
	day := dayOf(at)
	if j.file != nil && j.day == day {
		return false, nil
	}
	if j.file != nil {
		_ = j.file.Close()
		j.file = nil
		rotated = true
	}
	if err := os.MkdirAll(j.dir, journalDirMode); err != nil {
		return rotated, fmt.Errorf("create journal directory: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(j.dir, day), os.O_CREATE|os.O_WRONLY|os.O_APPEND, journalFileMode)
	if err != nil {
		return rotated, fmt.Errorf("open journal: %w", err)
	}
	j.file, j.day = f, day
	return rotated, nil
}

// Close releases both journal handles. A closed Store reopens on the
// next append, so Close is a resource hint rather than a lifecycle gate.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var firstErr error
	for _, j := range []*journal{s.tasks, s.workflow} {
		if j.file == nil {
			continue
		}
		if err := j.file.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		j.file, j.day = nil, ""
	}
	return firstErr
}
