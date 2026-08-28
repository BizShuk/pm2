package workflow

import (
	"strings"
	"testing"
)

func wf(category, name string, calls ...string) Config {
	stages := []Stage{{Name: "work", Script: "./work.sh"}}
	for i, target := range calls {
		stages = append(stages, Stage{Name: "call-" + target + string(rune('a'+i)), Workflow: target})
	}
	return Config{Category: category, Name: name, Stages: stages}
}

func defsOf(cfgs ...Config) map[string]Config {
	m := make(map[string]Config, len(cfgs))
	for _, c := range cfgs {
		m[c.Key()] = c
	}
	return m
}

func TestCheckAcyclicRejectsCycles(t *testing.T) {
	tests := []struct {
		name string
		defs map[string]Config
		want string
	}{
		{
			name: "self reference",
			defs: defsOf(wf("ci", "a", "ci:a")),
			want: "ci:a -> ci:a",
		},
		{
			name: "two node cycle",
			defs: defsOf(wf("ci", "a", "ci:b"), wf("ci", "b", "ci:a")),
			want: "ci:a -> ci:b -> ci:a",
		},
		{
			name: "three node cycle",
			defs: defsOf(wf("ci", "a", "ci:b"), wf("ci", "b", "ci:c"), wf("ci", "c", "ci:a")),
			want: "ci:a -> ci:b -> ci:c -> ci:a",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckAcyclic(tt.defs)
			if err == nil {
				t.Fatal("want a cycle error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("want path %q, got %q", tt.want, err)
			}
		})
	}
}

// TestCheckAcyclicAcceptsDiamond: sharing a child is not a cycle. A
// colouring that marked "already visited" as "on the path" would reject
// every workflow reused by two callers.
func TestCheckAcyclicAcceptsDiamond(t *testing.T) {
	defs := defsOf(
		wf("ci", "root", "ci:left", "ci:right"),
		wf("ci", "left", "ci:leaf"),
		wf("ci", "right", "ci:leaf"),
		wf("ci", "leaf"),
	)
	if err := CheckAcyclic(defs); err != nil {
		t.Errorf("a shared child is not a cycle: %v", err)
	}
}

// TestCycleMessageIsDeterministic guards against a message that depends
// on Go's randomised map iteration — the kind of flake that only shows
// up in CI, months later.
func TestCycleMessageIsDeterministic(t *testing.T) {
	defs := defsOf(
		wf("ci", "a", "ci:b"),
		wf("ci", "b", "ci:c"),
		wf("ci", "c", "ci:a"),
		wf("ci", "unrelated"),
		wf("other", "spare"),
	)
	first := CheckAcyclic(defs).Error()
	for i := 0; i < 100; i++ {
		if got := CheckAcyclic(defs).Error(); got != first {
			t.Fatalf("message drifted on run %d:\n first %q\n   got %q", i, first, got)
		}
	}
}

// TestDanglingRefIsNotACycle: a stage may name a workflow declared in a
// different ecosystem file, or applied later.
func TestDanglingRefIsNotACycle(t *testing.T) {
	defs := defsOf(wf("ci", "a", "ci:elsewhere"))
	if err := CheckAcyclic(defs); err != nil {
		t.Fatalf("unknown reference must not be a cycle: %v", err)
	}

	refs := DanglingRefs(defs)
	if len(refs) != 1 {
		t.Fatalf("want 1 dangling ref, got %d", len(refs))
	}
	if refs[0].Workflow != "ci:a" || refs[0].Target != "ci:elsewhere" || refs[0].Stage != 2 {
		t.Errorf("unexpected ref: %+v", refs[0])
	}
}

func TestResolve(t *testing.T) {
	defs := defsOf(
		wf("default", "deploy"),
		wf("ci", "deploy"),
		wf("ci", "unique"),
	)

	tests := []struct {
		ref  string
		want string
		ok   bool
	}{
		{"ci:deploy", "ci:deploy", true},
		{"ci:missing", "", false},
		{"deploy", "default:deploy", true}, // default category wins
		{"unique", "ci:unique", true},      // unique name across categories
		{"nosuch", "", false},
	}
	for _, tt := range tests {
		got, ok := Resolve(defs, tt.ref)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Resolve(%q) = (%q, %v), want (%q, %v)", tt.ref, got, ok, tt.want, tt.ok)
		}
	}
}

// TestResolveNeverGuessesOnAmbiguity: picking one of two same-named
// workflows would be a coin flip the caller cannot see.
func TestResolveAmbiguousBareNameFails(t *testing.T) {
	defs := defsOf(wf("ci", "build"), wf("prod", "build"))

	if _, ok := Resolve(defs, "build"); ok {
		t.Error("an ambiguous bare name must not resolve")
	}
	candidates := AmbiguousRef(defs, "build")
	if len(candidates) != 2 || candidates[0] != "ci:build" || candidates[1] != "prod:build" {
		t.Errorf("want the sorted candidates for the error message, got %v", candidates)
	}
}

func TestKeysAreSorted(t *testing.T) {
	got := Keys(defsOf(wf("z", "a"), wf("a", "z"), wf("m", "m")))
	want := []string{"a:z", "m:m", "z:a"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
}
