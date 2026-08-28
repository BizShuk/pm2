package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bizshuk/pm2/process"
)

func TestResolveScriptPath(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pm2-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test 1: Absolute path should remain unchanged
	absPath := "/usr/bin/node"
	res := process.ResolveScriptPath(tempDir, absPath)
	if res != absPath {
		t.Errorf("Expected %q, got %q", absPath, res)
	}

	// Test 2: Script path with separator should be resolved to absolute path
	relPath := "./bin/server"
	expectedAbs := filepath.Join(tempDir, relPath)
	res = process.ResolveScriptPath(tempDir, relPath)
	if res != expectedAbs {
		t.Errorf("Expected %q, got %q", expectedAbs, res)
	}

	// Test 3: Bare filename that exists in baseDir should be resolved
	scriptName := "run.sh"
	f, err := os.Create(filepath.Join(tempDir, scriptName))
	if err != nil {
		t.Fatalf("failed to create dummy script: %v", err)
	}
	f.Close()

	expectedAbs2 := filepath.Join(tempDir, scriptName)
	res = process.ResolveScriptPath(tempDir, scriptName)
	if res != expectedAbs2 {
		t.Errorf("Expected %q, got %q", expectedAbs2, res)
	}

	// Test 4: Bare filename that does not exist in baseDir but exists in PATH should be resolved to absolute path
	cmdName := "sh"
	expectedPath, err := exec.LookPath(cmdName)
	if err == nil {
		if abs, err := filepath.Abs(expectedPath); err == nil {
			expectedPath = abs
		}
		res = process.ResolveScriptPath(tempDir, cmdName)
		if res != expectedPath {
			t.Errorf("Expected %q, got %q", expectedPath, res)
		}
	}

	// Test 5: Bare filename that does not exist in baseDir nor in PATH should be left as-is
	nonExistentCmd := "nonexistentcommand12345"
	res = process.ResolveScriptPath(tempDir, nonExistentCmd)
	if res != nonExistentCmd {
		t.Errorf("Expected %q, got %q", nonExistentCmd, res)
	}
}

// TestLoadOptionalField pins the `optional` install-policy flag across
// both loader paths. The .js path crosses the goja boundary
// (exports -> JSON -> AppConfig), so a missing json tag would silently
// drop the field and make every optional app install by default.
func TestLoadOptionalField(t *testing.T) {
	dir := t.TempDir()

	jsPath := filepath.Join(dir, "ecosystem.config.js")
	js := `module.exports = { apps: [
    { name: "daily-report", script: "/bin/echo" },
    { name: "planner", script: "/bin/echo", optional: true }
] };`
	if err := os.WriteFile(jsPath, []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}

	jsonPath := filepath.Join(dir, "ecosystem.config.json")
	jsonSrc := `{"apps":[
    {"name":"daily-report","script":"/bin/echo"},
    {"name":"planner","script":"/bin/echo","optional":true}
]}`
	if err := os.WriteFile(jsonPath, []byte(jsonSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{jsPath, jsonPath} {
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load(%s): %v", filepath.Base(path), err)
		}
		if len(cfg.Apps) != 2 {
			t.Fatalf("%s: got %d apps, want 2", filepath.Base(path), len(cfg.Apps))
		}
		if cfg.Apps[0].Optional {
			t.Errorf("%s: daily-report should default to required", filepath.Base(path))
		}
		if !cfg.Apps[1].Optional {
			t.Errorf("%s: planner should be optional", filepath.Base(path))
		}
		for _, app := range cfg.Apps {
			if app.CWD != dir {
				t.Errorf("%s: %s CWD = %q, want config directory %q",
					filepath.Base(path), app.Name, app.CWD, dir)
			}
		}
	}
}

// --- workflows: ------------------------------------------------------

const workflowFixtureJS = `module.exports = {
    apps: [
        { name: "api", script: "./bin/api" }
    ],
    workflows: [
        {
            name: "nightly",
            category: "ci",
            cron: "0 2 * * *",
            timeout: "30m",
            env: { CI: "1" },
            stages: [
                { name: "pull", script: "./scripts/pull.sh", args: ["--ff-only"] },
                { name: "test", task: "unit-tests" },
                { name: "ship", workflow: "deploy" }
            ]
        },
        {
            name: "deploy",
            category: "ci",
            stages: [{ name: "push", script: "./scripts/push.sh" }]
        }
    ]
};`

const workflowFixtureJSON = `{
    "apps": [
        { "name": "api", "script": "./bin/api" }
    ],
    "workflows": [
        {
            "name": "nightly",
            "category": "ci",
            "cron": "0 2 * * *",
            "timeout": "30m",
            "env": { "CI": "1" },
            "stages": [
                { "name": "pull", "script": "./scripts/pull.sh", "args": ["--ff-only"] },
                { "name": "test", "task": "unit-tests" },
                { "name": "ship", "workflow": "deploy" }
            ]
        },
        {
            "name": "deploy",
            "category": "ci",
            "stages": [{ "name": "push", "script": "./scripts/push.sh" }]
        }
    ]
}`

func writeFixture(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// TestWorkflowsLoadIdenticallyFromJSAndJSON is the regression that keeps
// the two loaders converged. They already share one json.Unmarshal and
// now one postProcess; if either grows a branch the other lacks, the
// same fixture stops producing the same config.
func TestWorkflowsLoadIdenticallyFromJSAndJSON(t *testing.T) {
	dir := t.TempDir()
	jsPath := writeFixture(t, dir, "ecosystem.config.js", workflowFixtureJS)
	jsonPath := writeFixture(t, dir, "ecosystem.config.json", workflowFixtureJSON)

	fromJS, err := Load(jsPath)
	if err != nil {
		t.Fatalf("load js: %v", err)
	}
	fromJSON, err := Load(jsonPath)
	if err != nil {
		t.Fatalf("load json: %v", err)
	}

	// ConfigFile is the one field that legitimately differs.
	for i := range fromJS.Workflows {
		fromJS.Workflows[i].ConfigFile = ""
	}
	for i := range fromJSON.Workflows {
		fromJSON.Workflows[i].ConfigFile = ""
	}

	if !reflect.DeepEqual(fromJS.Workflows, fromJSON.Workflows) {
		t.Errorf("loaders drifted:\n  js:   %#v\n  json: %#v", fromJS.Workflows, fromJSON.Workflows)
	}
}

func TestWorkflowsNormalizedOnLoad(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "ecosystem.config.js", workflowFixtureJS)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Workflows) != 2 {
		t.Fatalf("want 2 workflows, got %d", len(cfg.Workflows))
	}

	nightly := cfg.Workflows[0]
	if nightly.Key() != "ci:nightly" {
		t.Errorf("key: got %q", nightly.Key())
	}
	if nightly.ConfigFile != path {
		t.Errorf("config file: want %q, got %q", path, nightly.ConfigFile)
	}
	if nightly.CWD != dir {
		t.Errorf("cwd should default to the config file's directory: got %q", nightly.CWD)
	}
	if want := filepath.Join(dir, "scripts/pull.sh"); nightly.Stages[0].Script != want {
		t.Errorf("script path: want %q, got %q", want, nightly.Stages[0].Script)
	}
	if nightly.Stages[1].Task != "unit-tests" {
		t.Errorf("task ref must not be path-resolved: got %q", nightly.Stages[1].Task)
	}
}

// TestInvalidWorkflowFailsAtLoad: a malformed stage must break
// `pm2 apply`, not the run that happens hours later.
func TestInvalidWorkflowFailsAtLoad(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "ecosystem.config.js", `module.exports = {
        workflows: [{ name: "broken", stages: [{ name: "s", script: "./a", task: "b" }] }]
    };`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("want a load error")
	}
	if !strings.Contains(err.Error(), "found: script, task") {
		t.Errorf("error should name what was found, got %q", err)
	}
}

func TestCyclicWorkflowFailsAtLoad(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "ecosystem.config.js", `module.exports = {
        workflows: [
            { name: "a", category: "ci", stages: [{ name: "s", workflow: "ci:b" }] },
            { name: "b", category: "ci", stages: [{ name: "s", workflow: "ci:a" }] }
        ]
    };`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("want a cycle error")
	}
	if !strings.Contains(err.Error(), "ci:a -> ci:b -> ci:a") {
		t.Errorf("error should name the cycle path, got %q", err)
	}
}

// TestAppsOnlyConfigUnchanged: a file that says nothing about workflows
// must behave exactly as it did before the field existed.
func TestAppsOnlyConfigUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "ecosystem.config.js",
		`module.exports = { apps: [{ name: "api", script: "./bin/api" }] };`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Apps) != 1 || cfg.Apps[0].Name != "api" {
		t.Errorf("apps: got %#v", cfg.Apps)
	}
	if cfg.Workflows != nil {
		t.Errorf("no workflows declared should stay nil, got %#v", cfg.Workflows)
	}
}
