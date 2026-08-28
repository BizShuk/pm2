package wizard

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bizshuk/pm2/config"
	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/workflow"
)

// appAnswers is one full askOneApp sequence taking every default, plus
// the "add another app?" refusal that ends the app block.
var appAnswers = append(make([]string, 12), "n")

func collectWith(t *testing.T, lines []string) (Ecosystem, string) {
	t.Helper()
	var out bytes.Buffer
	doc, err := collectAnswers(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out)
	if err != nil {
		t.Fatalf("collectAnswers: %v\n%s", err, out.String())
	}
	return doc, out.String()
}

// TestCollectAnswersWorkflowStages walks the whole workflow block: one
// script stage typed by hand and one task stage picked from the apps
// this same session declared.
func TestCollectAnswersWorkflowStages(t *testing.T) {
	lines := append(append([]string{}, appAnswers...),
		"y",       // add a workflow
		"ci",      // category
		"build",   // name
		"",        // cron
		"30m",     // timeout
		"",        // cwd
		"",        // env
		"1",       // stage type → script
		"unit",    // stage name
		"npm",     // script
		"test -q", // args
		"",        // stage env
		"",        // stage cwd
		"5m",      // stage timeout
		"y",       // add another stage
		"2",       // stage type → task
		"bounce",  // stage name
		"1",       // task → first app key
		"",        // stage timeout
		"n",       // add another stage
		"n",       // add another workflow
	)

	doc, out := collectWith(t, lines)
	if len(doc.Workflows) != 1 {
		t.Fatalf("expected 1 workflow, got %d\n%s", len(doc.Workflows), out)
	}
	wf := doc.Workflows[0]
	if wf.Category != "ci" || wf.Name != "build" {
		t.Errorf("key = %s:%s, want ci:build", wf.Category, wf.Name)
	}
	if wf.Timeout != "30m" {
		t.Errorf("timeout = %q, want 30m", wf.Timeout)
	}
	if len(wf.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d\n%s", len(wf.Stages), out)
	}

	unit := wf.Stages[0]
	if unit.Kind() != workflow.StageScript {
		t.Errorf("stage 1 kind = %q, want script", unit.Kind())
	}
	if unit.Script != "npm" || strings.Join(unit.Args, " ") != "test -q" {
		t.Errorf("stage 1 = %q %v, want npm [test -q]", unit.Script, unit.Args)
	}
	if unit.Timeout != "5m" {
		t.Errorf("stage 1 timeout = %q, want 5m", unit.Timeout)
	}

	bounce := wf.Stages[1]
	if bounce.Kind() != workflow.StageTask {
		t.Errorf("stage 2 kind = %q, want task", bounce.Kind())
	}
	if want := doc.TaskKeys()[0]; bounce.Task != want {
		t.Errorf("stage 2 task = %q, want %q", bounce.Task, want)
	}
}

// TestTaskStagePickerOffersDeclaredApps pins the reason the workflow
// block is collected after the apps: the picker's whole value is that
// the user does not have to remember a "<namespace>:<name>" key.
func TestTaskStagePickerOffersDeclaredApps(t *testing.T) {
	lines := append(append([]string{}, appAnswers...),
		"y", "", "", "", "", "", "", // workflow header, all defaults
		"2", "run", "1", "", "n", "n",
	)
	doc, out := collectWith(t, lines)
	key := doc.TaskKeys()[0]
	if !strings.Contains(out, key) {
		t.Fatalf("task picker did not offer %q:\n%s", key, out)
	}
	if got := doc.Workflows[0].Stages[0].Task; got != key {
		t.Errorf("task = %q, want %q", got, key)
	}
}

// TestWorkflowStageRequiresATarget documents that a stage with nothing
// to run is refused while the user can still answer, not after every
// other question has been asked.
func TestWorkflowStageRequiresATarget(t *testing.T) {
	lines := append(append([]string{}, appAnswers...),
		"y", "", "", "", "", "", "",
		"1", "empty", "", "", "", "", "",
	)
	var out bytes.Buffer
	_, err := collectAnswers(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out)
	if err == nil {
		t.Fatalf("expected an error for a stage with no script:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "script is required") {
		t.Errorf("error should name the missing script, got: %v", err)
	}
}

// TestRenderedWorkflowReloads is the round trip that matters: what the
// wizard writes must be what config.Load accepts, in both formats.
func TestRenderedWorkflowReloads(t *testing.T) {
	doc := Ecosystem{
		Apps: []process.AppConfig{DefaultApp()},
		Workflows: []workflow.Config{{
			Category: "ci",
			Name:     "build",
			Cron:     "0 3 * * *",
			Timeout:  "30m",
			Env:      map[string]string{"CI": "1"},
			Stages: []workflow.Stage{
				{Name: "unit", Script: "npm", Args: []string{"test"}, Timeout: "5m"},
				{Name: "bounce", Task: "Agent:AGENT APP - APP"},
			},
		}},
	}

	for _, tc := range []struct{ format, file string }{
		{FormatJS, "ecosystem.config.js"},
		{FormatJSON, "ecosystem.config.json"},
	} {
		data, err := renderEcosystem(doc, tc.format)
		if err != nil {
			t.Fatalf("%s: render: %v", tc.format, err)
		}
		path := filepath.Join(t.TempDir(), tc.file)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("%s: write: %v", tc.format, err)
		}
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatalf("%s: reload: %v\n%s", tc.format, err, data)
		}
		if len(cfg.Workflows) != 1 {
			t.Fatalf("%s: expected 1 workflow, got %d\n%s", tc.format, len(cfg.Workflows), data)
		}
		got := cfg.Workflows[0]
		if got.Key() != "ci:build" {
			t.Errorf("%s: key = %q, want ci:build", tc.format, got.Key())
		}
		if got.Cron != "0 3 * * *" || got.Timeout != "30m" {
			t.Errorf("%s: cron/timeout = %q/%q", tc.format, got.Cron, got.Timeout)
		}
		if len(got.Stages) != 2 {
			t.Fatalf("%s: expected 2 stages, got %d", tc.format, len(got.Stages))
		}
		if got.Stages[0].Kind() != workflow.StageScript {
			t.Errorf("%s: stage 1 kind = %q", tc.format, got.Stages[0].Kind())
		}
		if got.Stages[1].Task != "Agent:AGENT APP - APP" {
			t.Errorf("%s: stage 2 task = %q", tc.format, got.Stages[1].Task)
		}
	}
}

// TestRenderOmitsEmptyWorkflowBlock keeps the generated file honest for
// the overwhelmingly common case of a file with no workflow at all.
func TestRenderOmitsEmptyWorkflowBlock(t *testing.T) {
	doc := Ecosystem{Apps: []process.AppConfig{DefaultApp()}}

	js := renderEcosystemJS(doc)
	if strings.Contains(js, "workflows") {
		t.Errorf("js output mentions workflows with none declared:\n%s", js)
	}
	jsonOut, err := renderEcosystemJSON(doc)
	if err != nil {
		t.Fatalf("renderEcosystemJSON: %v", err)
	}
	if strings.Contains(jsonOut, "workflows") {
		t.Errorf("json output mentions workflows with none declared:\n%s", jsonOut)
	}
}

// TestRenderedWorkflowOmitsRuntimeFields pins the same boundary
// daemon/web's taskView pins: BaseEnv is a snapshot of the operator's
// shell, and it must never reach a file people commit.
func TestRenderedWorkflowOmitsRuntimeFields(t *testing.T) {
	doc := Ecosystem{Workflows: []workflow.Config{{
		Category:   "ci",
		Name:       "build",
		ConfigFile: "/tmp/other/ecosystem.config.js",
		BaseEnv:    []string{"AWS_SECRET_ACCESS_KEY=hunter2"},
		Stages:     []workflow.Stage{{Name: "unit", Script: "make"}},
	}}}

	js := renderEcosystemJS(doc)
	jsonOut, err := renderEcosystemJSON(doc)
	if err != nil {
		t.Fatalf("renderEcosystemJSON: %v", err)
	}
	for _, leak := range []string{"hunter2", "base_env", "config_file"} {
		if strings.Contains(js, leak) {
			t.Errorf("js output leaks %q:\n%s", leak, js)
		}
		if strings.Contains(jsonOut, leak) {
			t.Errorf("json output leaks %q:\n%s", leak, jsonOut)
		}
	}
}

// TestMergeDocumentsKeepsWorkflowsByKey checks the workflow half of the
// merge uses the daemon's identity, so two workflows sharing a name in
// different categories both survive.
func TestMergeDocumentsKeepsWorkflowsByKey(t *testing.T) {
	mk := func(category, name string) workflow.Config {
		return workflow.Config{
			Category: category,
			Name:     name,
			Stages:   []workflow.Stage{{Name: "s", Script: "true"}},
		}
	}
	existing := Ecosystem{Workflows: []workflow.Config{mk("ci", "build")}}
	incoming := Ecosystem{Workflows: []workflow.Config{
		mk("ci", "build"),      // duplicate key → skipped
		mk("nightly", "build"), // same name, other category → kept
	}}

	merged, counts := mergeDocuments(existing, incoming)
	if counts.workflowsSkipped != 1 {
		t.Errorf("workflowsSkipped = %d, want 1", counts.workflowsSkipped)
	}
	if len(merged.Workflows) != 2 {
		t.Fatalf("expected 2 workflows, got %d", len(merged.Workflows))
	}
	if merged.Workflows[1].Key() != "nightly:build" {
		t.Errorf("second key = %q, want nightly:build", merged.Workflows[1].Key())
	}
}

// TestWriteEcosystemFileRejectsCycle proves the wizard refuses to write
// a file it already knows pm2 apply will reject — and writes nothing.
func TestWriteEcosystemFileRejectsCycle(t *testing.T) {
	output := filepath.Join(t.TempDir(), "ecosystem.config.js")
	doc := Ecosystem{Workflows: []workflow.Config{
		{Category: "ci", Name: "a", Stages: []workflow.Stage{{Name: "s", Workflow: "ci:b"}}},
		{Category: "ci", Name: "b", Stages: []workflow.Stage{{Name: "s", Workflow: "ci:a"}}},
	}}
	ctx := WizardContext{
		In:     strings.NewReader(""),
		Out:    &bytes.Buffer{},
		ErrOut: &bytes.Buffer{},
		YesAll: true,
	}

	err := WriteEcosystemFile(ctx, doc, WriteOptions{Output: output})
	if err == nil {
		t.Fatal("expected a cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error should name the cycle, got: %v", err)
	}
	if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
		t.Errorf("invalid document was written anyway: %v", statErr)
	}
}

// TestWriteEcosystemFileWarnsOnDanglingTaskRef keeps the reference check
// advisory: the task may be registered from another file, so this is a
// warning on stderr and not a refusal.
func TestWriteEcosystemFileWarnsOnDanglingTaskRef(t *testing.T) {
	output := filepath.Join(t.TempDir(), "ecosystem.config.js")
	doc := Ecosystem{Workflows: []workflow.Config{{
		Category: "ci",
		Name:     "build",
		Stages:   []workflow.Stage{{Name: "s", Task: "nowhere:api"}},
	}}}
	var errOut bytes.Buffer
	ctx := WizardContext{
		In:     strings.NewReader(""),
		Out:    &bytes.Buffer{},
		ErrOut: &errOut,
		YesAll: true,
	}

	if err := WriteEcosystemFile(ctx, doc, WriteOptions{Output: output}); err != nil {
		t.Fatalf("WriteEcosystemFile: %v", err)
	}
	if !strings.Contains(errOut.String(), `references task "nowhere:api"`) {
		t.Errorf("missing dangling-task warning:\n%s", errOut.String())
	}
	if _, err := os.Stat(output); err != nil {
		t.Errorf("warning should not block the write: %v", err)
	}
}
