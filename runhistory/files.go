package runhistory

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// dayLayout names one journal file. Day files sort lexicographically,
// which is also chronologically — that is what lets a query walk them
// newest-first without parsing anything.
const dayLayout = "2006-01-02"

// dayFilePattern is the only thing a journal directory reader accepts.
// Anything else living in runs/ is ignored rather than parsed, matching
// logfile's rule that only a well-formed date makes a file ours.
var dayFilePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.jsonl$`)

// dayFiles returns the journal files in dir, newest first. A missing or
// unreadable directory is an empty result, not an error: a daemon that
// has never run a task has no journal at all, and that is an empty
// history rather than a failure.
func dayFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !dayFilePattern.MatchString(e.Name()) {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))

	paths := make([]string, 0, len(names))
	for _, n := range names {
		paths = append(paths, filepath.Join(dir, n))
	}
	return paths
}

// dayOf returns the journal file name for a moment in local time.
func dayOf(t time.Time) string { return t.Format(dayLayout) + ".jsonl" }
