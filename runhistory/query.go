package runhistory

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// DefaultLimit bounds a query that did not ask for one.
	DefaultLimit = 100

	// maxScanBytes caps how much of a single day file is read. A journal
	// that has grown past this is read from its tail, which is the half a
	// newest-first query wants anyway.
	maxScanBytes = 32 << 20
)

// Query selects records from a journal. The zero value means "the most
// recent DefaultLimit records of any name and status".
type Query struct {
	// Name matches a task's "ns:name" or bare name, or a workflow's
	// "category:name" or bare name. Empty matches everything.
	Name   string
	Status Status
	Limit  int
}

func (q Query) limit() int {
	if q.Limit <= 0 {
		return DefaultLimit
	}
	return q.Limit
}

// RecentTasks returns finished task records, newest first.
func (s *Store) RecentTasks(q Query) ([]TaskRecord, error) {
	return scan(TasksDir(s.root), q.limit(), func(r TaskRecord) bool {
		return matchName(q.Name, r.Namespace, r.Name) && matchStatus(q.Status, r.Status)
	})
}

// RecentWorkflows returns finished workflow records, newest first.
func (s *Store) RecentWorkflows(q Query) ([]WorkflowRecord, error) {
	return scan(WorkflowsDir(s.root), q.limit(), func(r WorkflowRecord) bool {
		return matchName(q.Name, r.Category, r.Name) && matchStatus(q.Status, r.Status)
	})
}

// WorkflowRun looks up one run by ID. A run ID carries its own date, so
// the ordinary case opens exactly one day file; an identifier from some
// other source falls back to scanning the directory.
func (s *Store) WorkflowRun(runID string) (WorkflowRecord, bool, error) {
	dir := WorkflowsDir(s.root)

	if day, ok := dayOfRunID(runID); ok {
		recs, err := scanFile(filepath.Join(dir, day+".jsonl"), 1, func(r WorkflowRecord) bool {
			return r.RunID == runID
		})
		if err != nil {
			return WorkflowRecord{}, false, err
		}
		if len(recs) > 0 {
			return recs[0], true, nil
		}
	}

	recs, err := scan(dir, 1, func(r WorkflowRecord) bool { return r.RunID == runID })
	if err != nil || len(recs) == 0 {
		return WorkflowRecord{}, false, err
	}
	return recs[0], true, nil
}

// StageLogPath names the file a workflow stage's output was written to.
func (s *Store) StageLogPath(workflow, runID, stage string) string {
	return filepath.Join(WorkflowLogsDir(s.root), stageLogName(workflow, runID, stage))
}

// StageLogName builds the basename stored in StageRecord.Log.
func StageLogName(workflow, runID, stage string) string {
	return stageLogName(workflow, runID, stage)
}

func stageLogName(workflow, runID, stage string) string {
	return safeComponent(workflow) + "." + runID + "." + safeComponent(stage) + ".log"
}

// safeComponent keeps a user-supplied name from steering a path out of
// the directory pm2 owns — the same reason process.TaskLogPath
// normalizes a task name.
func safeComponent(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "unnamed"
	}
	return b.String()
}

func matchName(want, namespace, name string) bool {
	if want == "" {
		return true
	}
	return want == name || want == namespace+":"+name
}

func matchStatus(want, got Status) bool { return want == "" || want == got }

// scan walks a journal directory newest-first and returns up to limit
// matching records.
func scan[T any](dir string, limit int, keep func(T) bool) ([]T, error) {
	var out []T
	for _, path := range dayFiles(dir) {
		recs, err := scanFile(path, limit-len(out), keep)
		if err != nil {
			return out, err
		}
		out = append(out, recs...)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// scanFile reads one day file and returns up to limit matching records,
// newest first.
//
// A malformed line is skipped, never fatal: a truncated final line is
// the ordinary shape of a journal whose writer was killed, and one bad
// byte must not make the whole history unreadable.
func scanFile[T any](path string, limit int, keep func(T) bool) ([]T, error) {
	if limit <= 0 {
		return nil, nil
	}
	data, err := readTail(path, maxScanBytes)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	lines := bytes.Split(data, []byte{'\n'})
	out := make([]T, 0, limit)
	for i := len(lines) - 1; i >= 0 && len(out) < limit; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		var rec T
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if keep(rec) {
			out = append(out, rec)
		}
	}
	return out, nil
}

// readTail returns the last max bytes of path, starting at a line
// boundary so a partially-read record is dropped rather than parsed.
func readTail(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() <= max {
		return io.ReadAll(f)
	}
	if _, err := f.Seek(info.Size()-max, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		data = data[i+1:]
	}
	return data, nil
}
