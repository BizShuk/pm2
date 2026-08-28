package workflow

import (
	"fmt"
	"strings"
)

// MaxNestingDepth bounds how deep nested workflow calls may go.
//
// Static validation already rejects every *declared* cycle, so hitting
// this limit means a pathological but legal chain. Eight is deeper than
// any real composition, and it bounds what a single trigger can spawn:
// goroutines, log files, and open file descriptors all scale with depth.
const MaxNestingDepth = 8

// Ref names one stage's reference to another workflow.
type Ref struct {
	Workflow string // the referring workflow's key
	Stage    int    // 1-based stage number
	Target   string // the reference as written
}

// CheckAcyclic reports the first cycle among the workflow stages of a
// definition set, keyed by Config.Key().
//
// It is called twice with different authority: once at config load, over
// a single file, for fast feedback; and once at daemon registration,
// over existing ∪ incoming, which is the check that actually binds. The
// CLI only ever sees one file, and a stage may legitimately reference a
// workflow declared elsewhere.
func CheckAcyclic(defs map[string]Config) error {
	const (
		white = 0 // unvisited
		grey  = 1 // on the current path
		black = 2 // fully explored
	)
	colour := make(map[string]int, len(defs))

	var stack []string
	var visit func(key string) error
	visit = func(key string) error {
		colour[key] = grey
		stack = append(stack, key)
		defer func() { stack = stack[:len(stack)-1] }()

		cfg := defs[key]
		for _, st := range cfg.Stages {
			if st.Kind() != StageWorkflow {
				continue
			}
			child, ok := resolve(defs, st.Workflow)
			if !ok {
				// A reference to a workflow this set does not contain is
				// not a cycle. It may be registered from another file, or
				// later; DanglingRefs reports it as a warning instead.
				continue
			}
			switch colour[child] {
			case grey:
				return fmt.Errorf("workflow cycle: %s", renderCycle(stack, child))
			case white:
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		colour[key] = black
		return nil
	}

	// Sorted traversal, so the cycle a set contains is always reported by
	// the same path. A message that depends on map iteration order is a
	// flaky test waiting to happen.
	for _, key := range Keys(defs) {
		if colour[key] == white {
			if err := visit(key); err != nil {
				return err
			}
		}
	}
	return nil
}

// renderCycle unwinds the current path back to the repeated node so the
// error names the loop itself rather than every workflow visited to
// reach it: "ci:a -> ci:b -> ci:a".
func renderCycle(stack []string, repeated string) string {
	start := 0
	for i, k := range stack {
		if k == repeated {
			start = i
			break
		}
	}
	return strings.Join(append(append([]string{}, stack[start:]...), repeated), " -> ")
}

// DanglingRefs lists workflow stages pointing at definitions the set
// does not contain. These are warnings, not errors: a workflow may be
// registered from a different ecosystem file, or applied later. A run
// fails the stage if the target is still missing then.
func DanglingRefs(defs map[string]Config) []Ref {
	var out []Ref
	for _, key := range Keys(defs) {
		for i, st := range defs[key].Stages {
			if st.Kind() != StageWorkflow {
				continue
			}
			if _, ok := resolve(defs, st.Workflow); !ok {
				out = append(out, Ref{Workflow: key, Stage: i + 1, Target: st.Workflow})
			}
		}
	}
	return out
}

// Resolve turns a reference into a definition key. A qualified
// "category:name" must match exactly; a bare name matches the default
// category first, then a unique name across all categories. An
// ambiguous bare name resolves to nothing rather than to an arbitrary
// pick — silently choosing one would be a coin flip the caller cannot
// see.
func Resolve(defs map[string]Config, ref string) (string, bool) { return resolve(defs, ref) }

func resolve(defs map[string]Config, ref string) (string, bool) {
	if category, name := ParseKey(ref); category != "" {
		key := category + ":" + name
		if _, ok := defs[key]; ok {
			return key, true
		}
		return "", false
	}

	if _, ok := defs[DefaultCategory+":"+ref]; ok {
		return DefaultCategory + ":" + ref, true
	}

	var match string
	var count int
	for _, key := range Keys(defs) {
		if defs[key].Name == ref {
			match, count = key, count+1
		}
	}
	if count == 1 {
		return match, true
	}
	return "", false
}

// AmbiguousRef lists every key a bare reference could have meant, for an
// error message that tells the user how to disambiguate.
func AmbiguousRef(defs map[string]Config, ref string) []string {
	var out []string
	for _, key := range Keys(defs) {
		if defs[key].Name == ref {
			out = append(out, key)
		}
	}
	return out
}
