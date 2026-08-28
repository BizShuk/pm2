package workflow

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStageKindRequiresExactlyOneForm(t *testing.T) {
	tests := []struct {
		name  string
		stage Stage
		want  StageKind
	}{
		{"script only", Stage{Script: "./run.sh"}, StageScript},
		{"task only", Stage{Task: "api"}, StageTask},
		{"workflow only", Stage{Workflow: "deploy"}, StageWorkflow},
		{"none", Stage{}, ""},
		{"script and task", Stage{Script: "./x", Task: "api"}, ""},
		{"script and workflow", Stage{Script: "./x", Workflow: "deploy"}, ""},
		{"task and workflow", Stage{Task: "api", Workflow: "deploy"}, ""},
		{"all three", Stage{Script: "./x", Task: "api", Workflow: "deploy"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.stage.Kind(); got != tt.want {
				t.Errorf("want %q, got %q", tt.want, got)
			}
		})
	}
}

// TestValidateNamesWhatWasFound: the error has to tell the user what
// they wrote, not only what was wanted.
func TestValidateNamesWhatWasFound(t *testing.T) {
	tests := []struct {
		name  string
		stage Stage
		want  string
	}{
		{"none", Stage{Name: "s"}, "found: none"},
		{"two", Stage{Name: "s", Script: "./x", Task: "api"}, "found: script, task"},
		{"three", Stage{Name: "s", Script: "./x", Task: "a", Workflow: "b"}, "found: script, task, workflow"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Category: "ci", Name: "w", Stages: []Stage{tt.stage}}
			err := cfg.Validate()
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("want message containing %q, got %q", tt.want, err)
			}
		})
	}
}

// TestScriptOnlyFieldsRejectedElsewhere: a field that cannot take effect
// is a mistake, not something to ignore.
func TestScriptOnlyFieldsRejectedElsewhere(t *testing.T) {
	tests := []struct {
		name  string
		stage Stage
	}{
		{"args on task", Stage{Name: "s", Task: "api", Args: []string{"--x"}}},
		{"env on task", Stage{Name: "s", Task: "api", Env: map[string]string{"A": "1"}}},
		{"args on workflow", Stage{Name: "s", Workflow: "deploy", Args: []string{"--x"}}},
		{"env on workflow", Stage{Name: "s", Workflow: "deploy", Env: map[string]string{"A": "1"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Category: "ci", Name: "w", Stages: []Stage{tt.stage}}
			if err := cfg.Validate(); err == nil {
				t.Error("want an error; a silently ignored override is worse than a rejection")
			}
		})
	}
}

func TestValidateRejectsStructuralProblems(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "unnamed workflow",
			cfg:  Config{Category: "ci", Stages: []Stage{{Name: "a", Script: "./x"}}},
			want: "name is required",
		},
		{
			name: "no stages",
			cfg:  Config{Category: "ci", Name: "w"},
			want: "at least one stage",
		},
		{
			name: "duplicate stage names",
			cfg: Config{Category: "ci", Name: "w", Stages: []Stage{
				{Name: "build", Script: "./a"}, {Name: "build", Script: "./b"},
			}},
			want: "repeats the name",
		},
		{
			name: "bad workflow timeout",
			cfg:  Config{Category: "ci", Name: "w", Timeout: "30 minutes", Stages: []Stage{{Name: "a", Script: "./x"}}},
			want: "timeout",
		},
		{
			name: "bad stage timeout",
			cfg:  Config{Category: "ci", Name: "w", Stages: []Stage{{Name: "a", Script: "./x", Timeout: "soon"}}},
			want: "timeout",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("want message containing %q, got %q", tt.want, err)
			}
		})
	}
}

func TestValidateAcceptsAWellFormedWorkflow(t *testing.T) {
	cfg := Config{
		Category: "ci", Name: "nightly", Timeout: "30m",
		Stages: []Stage{
			{Name: "pull", Script: "./pull.sh", Args: []string{"--ff-only"}},
			{Name: "test", Task: "unit-tests"},
			{Name: "ship", Workflow: "deploy", Timeout: "5m"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("want valid, got %v", err)
	}
}

// TestNormalizeResolvesScriptsOnly: a task or workflow reference is a
// name, not a path. Rewriting it would turn "unit-tests" into a
// directory entry that does not exist.
func TestNormalizeResolvesScriptsOnly(t *testing.T) {
	base := t.TempDir()
	cfg := Config{
		Name: "w",
		Stages: []Stage{
			{Name: "a", Script: "./scripts/run.sh"},
			{Name: "b", Task: "unit-tests"},
			{Name: "c", Workflow: "deploy"},
		},
	}
	cfg.Normalize(base)

	if want := filepath.Join(base, "scripts/run.sh"); cfg.Stages[0].Script != want {
		t.Errorf("script: want %q, got %q", want, cfg.Stages[0].Script)
	}
	if cfg.Stages[1].Task != "unit-tests" {
		t.Errorf("task ref was rewritten: %q", cfg.Stages[1].Task)
	}
	if cfg.Stages[2].Workflow != "deploy" {
		t.Errorf("workflow ref was rewritten: %q", cfg.Stages[2].Workflow)
	}
}

func TestNormalizeDefaults(t *testing.T) {
	base := t.TempDir()
	cfg := Config{Name: "w", Stages: []Stage{{Script: "./a"}, {Script: "./b"}}}
	cfg.Normalize(base)

	if cfg.Category != DefaultCategory {
		t.Errorf("category: want %q, got %q", DefaultCategory, cfg.Category)
	}
	if cfg.CWD != base {
		t.Errorf("cwd: want %q, got %q", base, cfg.CWD)
	}
	if cfg.Stages[0].Name != "stage-1" || cfg.Stages[1].Name != "stage-2" {
		t.Errorf("stage names: got %q, %q", cfg.Stages[0].Name, cfg.Stages[1].Name)
	}
	for i, st := range cfg.Stages {
		if st.CWD != base {
			t.Errorf("stage %d cwd: want %q, got %q", i, base, st.CWD)
		}
	}
	// Normalize must not invent a workflow name the way an app derives
	// one from its script filename — there is no single script to derive from.
	unnamed := Config{Stages: []Stage{{Script: "./a"}}}
	unnamed.Normalize(base)
	if unnamed.Name != "" {
		t.Errorf("Normalize must not invent a name, got %q", unnamed.Name)
	}
}

func TestTimeoutDurationFallsBackToWorkflow(t *testing.T) {
	cfg := Config{Timeout: "30m"}
	if got := cfg.TimeoutDuration(Stage{}); got.String() != "30m0s" {
		t.Errorf("inherit: want 30m0s, got %s", got)
	}
	if got := cfg.TimeoutDuration(Stage{Timeout: "5s"}); got.String() != "5s" {
		t.Errorf("override: want 5s, got %s", got)
	}
	if got := (Config{}).TimeoutDuration(Stage{}); got != 0 {
		t.Errorf("no limit: want 0, got %s", got)
	}
}

func TestValidateAllRejectsDuplicateKeys(t *testing.T) {
	cfgs := []Config{
		{Category: "ci", Name: "w", ConfigFile: "a.js", Stages: []Stage{{Name: "s", Script: "./x"}}},
		{Category: "ci", Name: "w", ConfigFile: "b.js", Stages: []Stage{{Name: "s", Script: "./y"}}},
	}
	err := ValidateAll(cfgs)
	if err == nil || !strings.Contains(err.Error(), "declared twice") {
		t.Errorf("want a duplicate-key error, got %v", err)
	}
}

func TestParseKey(t *testing.T) {
	if c, n := ParseKey("ci:nightly"); c != "ci" || n != "nightly" {
		t.Errorf("qualified: got (%q, %q)", c, n)
	}
	if c, n := ParseKey("nightly"); c != "" || n != "nightly" {
		t.Errorf("bare: got (%q, %q)", c, n)
	}
}
