package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bizshuk/pm2/config"
	"github.com/spf13/cobra"
)

func TestRenderEcosystemJSEmpty(t *testing.T) {
	out := renderEcosystemJS(nil)
	if !strings.Contains(out, "module.exports = {") {
		t.Errorf("Expected module.exports header, got %q", out)
	}
	if !strings.Contains(out, "apps: [") {
		t.Errorf("Expected empty apps array, got %q", out)
	}
}

func TestRenderEcosystemJSSingle(t *testing.T) {
	app := config.AppConfig{
		Name:      "api",
		Script:    "./bin/server",
		Args:      []string{"--port", "8080"},
		Namespace: "default",
		Instances: 2,
		Env:       map[string]string{"NODE_ENV": "production"},
	}
	app.Normalize()

	got := renderEcosystemJS([]config.AppConfig{app})

	// Spot-check key lines (don't lock the entire template to ease future tweaks).
	mustContain := []string{
		"module.exports = {",
		"apps: [",
		`name: "api"`,
		`script: "./bin/server"`,
		`args: ["--port", "8080"]`,
		`namespace: "default"`,
		"instances: 2",
		"env: {",
		`"NODE_ENV": "production"`,
		"};",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("Expected output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestRenderEcosystemJSSkipsEmpty(t *testing.T) {
	app := config.AppConfig{
		Name:   "minimal",
		Script: "app.js",
	}
	app.Normalize()

	got := renderEcosystemJS([]config.AppConfig{app})

	skip := []string{
		"args:",
		"env:",
		"watch:",
		"cron_restart:",
		"cron:",
	}
	for _, s := range skip {
		if strings.Contains(got, s) {
			t.Errorf("Expected output to omit %q, got:\n%s", s, got)
		}
	}
}

func TestRenderEcosystemJSON(t *testing.T) {
	app := config.AppConfig{
		Name:      "worker",
		Script:    "worker.js",
		Args:      []string{"--q"},
		Namespace: "jobs",
		Instances: 3,
		Watch:     true,
		Env:       map[string]string{"FOO": "bar"},
	}
	app.Normalize()

	out, err := renderEcosystemJSON([]config.AppConfig{app})
	if err != nil {
		t.Fatalf("renderEcosystemJSON: %v", err)
	}
	var cfg config.EcosystemConfig
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("output is not valid JSON: %v\nout:\n%s", err, out)
	}
	if len(cfg.Apps) != 1 {
		t.Fatalf("Expected 1 app, got %d", len(cfg.Apps))
	}
	got := cfg.Apps[0]
	if got.Name != "worker" || got.Script != "worker.js" || got.Instances != 3 ||
		!got.Watch || got.Env["FOO"] != "bar" || got.Namespace != "jobs" {
		t.Errorf("Round-trip mismatch: got %+v", got)
	}
}

func TestRenderRoundTrip(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pm2-test")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cases := []struct {
		name string
		apps []config.AppConfig
	}{
		{
			name: "single",
			apps: []config.AppConfig{
				{Name: "api", Script: "./server.js", Instances: 1, Namespace: "default"},
			},
		},
		{
			name: "multi",
			apps: []config.AppConfig{
				{Name: "api", Script: "./a.js", Instances: 2, Namespace: "default", Watch: true},
				{Name: "worker", Script: "./b.js", Instances: 1, Namespace: "jobs",
					Env: map[string]string{"K": "V"}},
			},
		},
		{
			name: "cron",
			apps: []config.AppConfig{
				{Name: "sched", Script: "./c.js", Cron: "0 * * * *", CronRestart: "0 3 * * *"},
			},
		},
	}

	for _, tc := range cases {
		// Normalize inputs to match what the loader will produce.
		for i := range tc.apps {
			tc.apps[i].Normalize()
		}

		js := renderEcosystemJS(tc.apps)
		path := filepath.Join(tempDir, "eco.js")
		if err := os.WriteFile(path, []byte(js), 0o644); err != nil {
			t.Fatalf("%s: write: %v", tc.name, err)
		}
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatalf("%s: load: %v\njs:\n%s", tc.name, err, js)
		}
		if len(cfg.Apps) != len(tc.apps) {
			t.Errorf("%s: expected %d apps, got %d", tc.name, len(tc.apps), len(cfg.Apps))
			continue
		}
		for i := range tc.apps {
			want, got := tc.apps[i], cfg.Apps[i]
			// Note: Script path may be absolute-ised by config.Load's
			// resolveScriptPath when the file actually exists in the test
			// directory, so we compare by base name only.
			if want.Name != got.Name || want.Instances != got.Instances ||
				want.Namespace != got.Namespace || want.Watch != got.Watch ||
				want.Cron != got.Cron || want.CronRestart != got.CronRestart {
				t.Errorf("%s[%d]: mismatch want=%+v got=%+v", tc.name, i, want, got)
			}
			if filepath.Base(want.Script) != filepath.Base(got.Script) {
				t.Errorf("%s[%d]: script base want=%q got=%q", tc.name, i, filepath.Base(want.Script), filepath.Base(got.Script))
			}
			if len(want.Env) != len(got.Env) {
				t.Errorf("%s[%d]: env count want=%d got=%d", tc.name, i, len(want.Env), len(got.Env))
			}
			for k, v := range want.Env {
				if got.Env[k] != v {
					t.Errorf("%s[%d]: env[%q] want=%q got=%q", tc.name, i, k, v, got.Env[k])
				}
			}
		}
	}
}

func TestRenderEscapes(t *testing.T) {
	weird := "weird\"name\\path"
	app := config.AppConfig{
		Name:   "escape-test",
		Script: weird,
	}
	app.Normalize()

	tempDir, err := os.MkdirTemp("", "pm2-test")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	js := renderEcosystemJS([]config.AppConfig{app})
	path := filepath.Join(tempDir, "eco.js")
	if err := os.WriteFile(path, []byte(js), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v\njs:\n%s", err, js)
	}
	if cfg.Apps[0].Script != weird {
		t.Errorf("Expected script %q, got %q", weird, cfg.Apps[0].Script)
	}
}

func TestCollectAnswersSingle(t *testing.T) {
	// askOneApp prompt sequence (all blanks → defaults):
	//   1. script
	//   2. name
	//   3. args
	//   4. namespace
	//   5. instances
	//   6. watch
	//   7. env (blank → finish; reads 1 line)
	//   8. cron (blank → skip restart)
	// Then collectAnswers prompt: "Add another app?" → "n"
	// Total: 9 lines of input.
	lines := []string{
		"name-only-one", // script
		"",              // name (derive)
		"",              // args
		"",              // namespace
		"",              // instances
		"",              // watch
		"",              // env (blank → finish)
		"",              // cron
		"n",             // add another? → no
	}
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	var out bytes.Buffer

	apps, err := collectAnswers(in, &out)
	if err != nil {
		t.Fatalf("collectAnswers: %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("Expected 1 app, got %d (out:\n%s)", len(apps), out.String())
	}
	got := apps[0]
	if got.Script != "name-only-one" {
		t.Errorf("Expected script 'name-only-one', got %q", got.Script)
	}
	if got.Name != "name-only-one" {
		t.Errorf("Expected derived name 'name-only-one', got %q", got.Name)
	}
	if got.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got %q", got.Namespace)
	}
	if got.Instances != 1 {
		t.Errorf("Expected instances 1, got %d", got.Instances)
	}
	if got.Watch {
		t.Errorf("Expected watch false")
	}
	if got.Cron != "" || got.CronRestart != "" {
		t.Errorf("Expected no cron, got cron=%q restart=%q", got.Cron, got.CronRestart)
	}
}

// TestCollectAnswersMulti covers two apps in a row.
func TestCollectAnswersMulti(t *testing.T) {
	// Each app: 8 lines (script, name, args, namespace, instances, watch,
	// env-blank, cron-blank). Between apps: 1 line "y" / "n".
	input := strings.Join([]string{
		"first.js",  // 1: script
		"",          // 2: name
		"",          // 3: args
		"",          // 4: namespace
		"",          // 5: instances
		"",          // 6: watch
		"",          // 7: env (blank → finish)
		"",          // 8: cron
		"y",         // add another? → yes
		"second.js", // 1: script
		"second",    // 2: name
		"",          // 3: args
		"",          // 4: namespace
		"",          // 5: instances
		"",          // 6: watch
		"",          // 7: env
		"",          // 8: cron
		"n",         // add another? → no
	}, "\n")
	in := strings.NewReader(input + "\n")
	var out bytes.Buffer

	apps, err := collectAnswers(in, &out)
	if err != nil {
		t.Fatalf("collectAnswers: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("Expected 2 apps, got %d (out:\n%s)", len(apps), out.String())
	}
	if apps[0].Script != "first.js" {
		t.Errorf("apps[0].Script = %q, want %q", apps[0].Script, "first.js")
	}
	if apps[1].Script != "second.js" {
		t.Errorf("apps[1].Script = %q, want %q", apps[1].Script, "second.js")
	}
	if apps[1].Name != "second" {
		t.Errorf("apps[1].Name = %q, want %q", apps[1].Name, "second")
	}
}

func TestPromptYesNoDefaults(t *testing.T) {
	var buf bytes.Buffer

	// Empty input → def=true
	got, err := promptYesNo(bufio.NewReader(strings.NewReader("\n")), &buf, "Q?", true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !got {
		t.Errorf("Expected default true on empty input")
	}
	buf.Reset()

	// Empty input → def=false
	got, err = promptYesNo(bufio.NewReader(strings.NewReader("\n")), &buf, "Q?", false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got {
		t.Errorf("Expected default false on empty input")
	}
	buf.Reset()

	// "y" → true regardless of def
	got, err = promptYesNo(bufio.NewReader(strings.NewReader("y\n")), &buf, "Q?", false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !got {
		t.Errorf("Expected true on 'y'")
	}
	buf.Reset()

	// "no" → false regardless of def
	got, err = promptYesNo(bufio.NewReader(strings.NewReader("no\n")), &buf, "Q?", true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got {
		t.Errorf("Expected false on 'no'")
	}
}

// inOutString is a no-op helper kept for clarity; current tests use
// strings.NewReader directly so this is unused but documents intent.
func inOutString(r *strings.Reader) string { return "" }

func TestDeriveName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"app.js", "app"},
		{"./bin/server", "server"},
		{"/abs/path/worker.ts", "worker"},
		{"noext", "noext"},
		{"", ecoDefaultName},
	}
	for _, c := range cases {
		if got := deriveName(c.in); got != c.want {
			t.Errorf("deriveName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---------- loadExistingApps ----------

func TestLoadExistingAppsNotFound(t *testing.T) {
	dir := t.TempDir()
	apps, err := loadExistingApps(filepath.Join(dir, "absent.js"))
	if err != nil {
		t.Fatalf("expected nil err for missing file, got %v", err)
	}
	if apps != nil {
		t.Errorf("expected nil apps for missing file, got %v", apps)
	}
}

func TestLoadExistingAppsJS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eco.js")
	src := `module.exports = { apps: [ { name: "api", script: "./a.js" } ] };`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	apps, err := loadExistingApps(path)
	if err != nil {
		t.Fatalf("loadExistingApps: %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}
	if apps[0].Name != "api" {
		t.Errorf("expected name=api, got %q", apps[0].Name)
	}
}

func TestLoadExistingAppsJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eco.json")
	src := `{ "apps": [ { "name": "worker", "script": "./w.js" } ] }`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	apps, err := loadExistingApps(path)
	if err != nil {
		t.Fatalf("loadExistingApps: %v", err)
	}
	if len(apps) != 1 || apps[0].Name != "worker" {
		t.Errorf("unexpected apps: %+v", apps)
	}
}

func TestLoadExistingAppsMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eco.js")
	if err := os.WriteFile(path, []byte("module.exports = { apps: ["), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := loadExistingApps(path)
	if err == nil {
		t.Fatal("expected error for malformed file, got nil")
	}
}

func TestLoadExistingAppsBadExt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eco.txt")
	if err := os.WriteFile(path, []byte("not a config"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := loadExistingApps(path)
	if err == nil {
		t.Fatal("expected error for unsupported extension, got nil")
	}
}

// ---------- mergeAppsByName ----------

func mkApp(name, script string) config.AppConfig {
	a := config.AppConfig{Name: name, Script: script}
	a.Normalize()
	return a
}

func TestMergeAppsByNameNoCollision(t *testing.T) {
	existing := []config.AppConfig{mkApp("api", "a.js"), mkApp("worker", "w.js")}
	newApps := []config.AppConfig{mkApp("cron", "c.js")}
	merged, skipped := mergeAppsByName(existing, newApps)
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}
	if len(merged) != 3 {
		t.Fatalf("len(merged) = %d, want 3", len(merged))
	}
	names := []string{merged[0].Name, merged[1].Name, merged[2].Name}
	if names[0] != "api" || names[1] != "worker" || names[2] != "cron" {
		t.Errorf("merge order: %v", names)
	}
}

func TestMergeAppsByNameSkipDuplicate(t *testing.T) {
	existing := []config.AppConfig{
		{Name: "api", Script: "a.js", Instances: 4, Env: map[string]string{"K": "v"}},
		mkApp("worker", "w.js"),
	}
	newApps := []config.AppConfig{
		mkApp("worker", "different.js"),
		mkApp("cron", "c.js"),
	}
	merged, skipped := mergeAppsByName(existing, newApps)
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	if len(merged) != 3 {
		t.Fatalf("len(merged) = %d, want 3", len(merged))
	}
	// Existing "worker" wins — original Script preserved.
	apiIdx, workerIdx := -1, -1
	for i, a := range merged {
		switch a.Name {
		case "api":
			apiIdx = i
		case "worker":
			workerIdx = i
		}
	}
	if apiIdx < 0 || workerIdx < 0 {
		t.Fatalf("expected api+worker in merged, got %+v", merged)
	}
	if merged[workerIdx].Script != "w.js" {
		t.Errorf("existing worker overwritten: script=%q", merged[workerIdx].Script)
	}
	if merged[apiIdx].Instances != 4 || merged[apiIdx].Env["K"] != "v" {
		t.Errorf("existing api fields lost: %+v", merged[apiIdx])
	}
}

func TestMergeAppsByNameAllDuplicates(t *testing.T) {
	existing := []config.AppConfig{mkApp("api", "a.js")}
	newApps := []config.AppConfig{mkApp("api", "a2.js"), mkApp("api", "a3.js")}
	merged, skipped := mergeAppsByName(existing, newApps)
	if skipped != 2 {
		t.Errorf("skipped = %d, want 2", skipped)
	}
	if len(merged) != 1 {
		t.Errorf("len(merged) = %d, want 1", len(merged))
	}
	if merged[0].Script != "a.js" {
		t.Errorf("existing api script changed: %q", merged[0].Script)
	}
}

// ---------- detectFormatFromExt ----------

func TestDetectFormatFromExt(t *testing.T) {
	cases := []struct {
		path    string
		wantFmt string
		wantOK  bool
	}{
		{"eco.js", "js", true},
		{"eco.cjs", "js", true},
		{"eco.mjs", "js", true},
		{"eco.json", "json", true},
		{"eco.yaml", "", false},
		{"eco", "", false},
		{"/abs/path/ECO.JSON", "json", true},
	}
	for _, c := range cases {
		gotFmt, gotOK := detectFormatFromExt(c.path)
		if gotFmt != c.wantFmt || gotOK != c.wantOK {
			t.Errorf("detectFormatFromExt(%q) = (%q,%v), want (%q,%v)",
				c.path, gotFmt, gotOK, c.wantFmt, c.wantOK)
		}
	}
}

// ---------- end-to-end wizard merge flow ----------

// runWizard invokes the wizard cobra command with the given args, piping
// stdin for the interactive prompts. Returns the resulting output file
// contents (or "" if not written) and the run error.
func runWizard(t *testing.T, dir, stdin, args string) (string, error) {
	t.Helper()
	path := filepath.Join(dir, "ecosystem.config.js")

	// Mock the TTY check so piped stdin is accepted as interactive.
	// Save and restore the package var so other tests are unaffected.
	prev := isTerminalFunc
	isTerminalFunc = func(fd uintptr) bool { return true }
	t.Cleanup(func() { isTerminalFunc = prev })

	// Build a fresh root command each call to avoid state pollution
	// from cobra's flag-default caching and the global metric hook.
	root := newRootForTest()
	root.SetArgs(append([]string{"wizard"}, strings.Fields(args)...))
	root.SetIn(strings.NewReader(stdin))
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)

	err := root.Execute()
	if err != nil {
		return "", err
	}
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		return "", nil
	}
	return string(data), nil
}

// newRootForTest returns a bare cobra root containing only the wizard
// command. Kept here (not in root.go) so production init() side effects
// (e.g. metric hook, default pm2Home) don't leak into tests.
func newRootForTest() *cobra.Command {
	root := &cobra.Command{Use: "pm2"}
	root.AddCommand(newEcoCmd())
	return root
}

func TestWizardEndToEndMerge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ecosystem.config.js")
	seed := `module.exports = { apps: [ { name: "api", script: "./a.js" } ] };`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// askOneApp prompt sequence (blanks → defaults), then "n" to stop:
	//   script, name, args, namespace, instances, watch, env-blank, cron-blank
	//   → "n"
	stdin := strings.Join([]string{
		"worker.js", // script
		"",          // name → derive "worker"
		"",          // args
		"",          // namespace
		"",          // instances
		"",          // watch
		"",          // env
		"",          // cron
		"n",         // add another? → no
	}, "\n") + "\n"

	got, err := runWizard(t, dir, stdin,
		"--output "+path)
	if err != nil {
		t.Fatalf("wizard: %v", err)
	}
	if !strings.Contains(got, `name: "api"`) {
		t.Errorf("merged output missing 'api':\n%s", got)
	}
	if !strings.Contains(got, `name: "worker"`) {
		t.Errorf("merged output missing 'worker':\n%s", got)
	}
}

func TestWizardEndToEndForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ecosystem.config.js")
	seed := `module.exports = { apps: [
		{ name: "old-a", script: "./a.js" },
		{ name: "old-b", script: "./b.js" },
	] };`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := runWizard(t, dir, "\n", "--output "+path+" --yes --force")
	if err != nil {
		t.Fatalf("wizard: %v", err)
	}
	if strings.Contains(got, "old-a") || strings.Contains(got, "old-b") {
		t.Errorf("--force did not replace existing apps:\n%s", got)
	}
	if !strings.Contains(got, "name: \"app\"") {
		t.Errorf("expected default app name, got:\n%s", got)
	}
}

func TestWizardEndToEndMalformedAbort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ecosystem.config.js")
	if err := os.WriteFile(path, []byte("module.exports = { apps: ["), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := runWizard(t, dir, "\n", "--output "+path+" --yes")
	if err == nil {
		t.Fatal("expected abort on malformed existing file")
	}
	if !strings.Contains(err.Error(), "use --force") {
		t.Errorf("error should mention --force, got: %v", err)
	}
}

func TestWizardEndToEndNoMerge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ecosystem.config.js")
	seed := `module.exports = { apps: [ { name: "api", script: "./a.js" } ] };`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := runWizard(t, dir, "\n", "--output "+path+" --yes --no-merge")
	if err == nil {
		t.Fatal("expected abort with --no-merge on existing file")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should mention --force, got: %v", err)
	}
}

func TestWizardEndToEndNoMergeWithForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ecosystem.config.js")
	seed := `module.exports = { apps: [ { name: "old", script: "./o.js" } ] };`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := runWizard(t, dir, "\n", "--output "+path+" --yes --no-merge --force")
	if err != nil {
		t.Fatalf("wizard: %v", err)
	}
	if strings.Contains(got, "old") {
		t.Errorf("--no-merge --force did not overwrite:\n%s", got)
	}
	if !strings.Contains(got, "name: \"app\"") {
		t.Errorf("expected default app, got:\n%s", got)
	}
}

// ---------- wizard install ----------

func TestPlannerPrefixes(t *testing.T) {
	if ecoPlannerSystemPrefix != "run /system-planner for current workspace, and output under <workspace>/plans/" {
		t.Errorf("ecoPlannerSystemPrefix = %q", ecoPlannerSystemPrefix)
	}
	if ecoPlannerBusinessPrefix != "run /business-planner for current workspace, and output under <workspace>/plans/" {
		t.Errorf("ecoPlannerBusinessPrefix = %q", ecoPlannerBusinessPrefix)
	}
}

func TestBuildInstallApp(t *testing.T) {
	app := buildInstallApp("/abs/agy", ecoPlannerSystemPrefix, "analyze repo", ecoPlannerNS, "pm2", "/home/user/pm2")
	if app.Script != "/abs/agy" {
		t.Errorf("Script = %q, want /abs/agy", app.Script)
	}
	if app.Name != "agy-pm2" {
		t.Errorf("Name = %q, want agy-pm2", app.Name)
	}
	if app.Namespace != ecoPlannerNS {
		t.Errorf("Namespace = %q, want %q", app.Namespace, ecoPlannerNS)
	}
	if app.CWD != "/home/user/pm2" {
		t.Errorf("CWD = %q, want /home/user/pm2", app.CWD)
	}
	// agy is a planner agent → --add-dir <cwd> prepended; prefix+prompt
	// joined into one single-quoted -p arg.
	wantArgs := []string{
		"--add-dir", "/home/user/pm2",
		"-p", "'" + ecoPlannerSystemPrefix + " analyze repo'",
	}
	if len(app.Args) != len(wantArgs) {
		t.Fatalf("len(Args) = %d, want %d", len(app.Args), len(wantArgs))
	}
	for i, a := range wantArgs {
		if app.Args[i] != a {
			t.Errorf("Args[%d] = %q, want %q", i, app.Args[i], a)
		}
	}
	if app.Instances != 1 {
		t.Errorf("Instances = %d, want 1", app.Instances)
	}
}

func TestBuildInstallAppEmptyUserPrompt(t *testing.T) {
	app := buildInstallApp("/abs/agy", ecoPlannerBusinessPrefix, "", ecoPlannerNS, "myproj", "/home/user/proj")
	// Empty user_prompt → prompt is just the prefix, still single-quoted.
	wantArgs := []string{
		"--add-dir", "/home/user/proj",
		"-p", "'" + ecoPlannerBusinessPrefix + "'",
	}
	if len(app.Args) != len(wantArgs) {
		t.Fatalf("len(Args) = %d, want %d", len(app.Args), len(wantArgs))
	}
	for i, a := range wantArgs {
		if app.Args[i] != a {
			t.Errorf("Args[%d] = %q, want %q", i, app.Args[i], a)
		}
	}
	if app.Name != "agy-myproj" {
		t.Errorf("Name = %q, want agy-myproj", app.Name)
	}
	if app.Namespace != ecoPlannerNS {
		t.Errorf("Namespace = %q, want %q", app.Namespace, ecoPlannerNS)
	}
}

// buildInstallApp should drop the cwd suffix entirely when cwdBasename
// is empty (defensive guard for unusual Getwd failures).
func TestBuildInstallAppEmptyCwdBasename(t *testing.T) {
	app := buildInstallApp("/abs/agy", ecoPlannerSystemPrefix, "x", ecoPlannerNS, "", "/abs/cwd")
	if app.Name != "agy" {
		t.Errorf("Name = %q, want agy (no suffix when cwdBasename empty)", app.Name)
	}
}

// runInstall invokes the install subcommand on a fresh root. The
// args slice is passed straight through to cobra so callers can use
// any characters (spaces, quotes, etc.) without a shell parser. The
// process CWD is changed to dir for the duration of the call so
// the default --output path lands inside the temp dir.
func runInstall(t *testing.T, dir string, args []string) (string, string, error) {
	t.Helper()
	path := filepath.Join(dir, "ecosystem.config.js")

	prevCwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		t.Fatalf("getwd: %v", cwdErr)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevCwd) })

	prev := isTerminalFunc
	isTerminalFunc = func(fd uintptr) bool { return true }
	t.Cleanup(func() { isTerminalFunc = prev })

	realDir, _ := os.Getwd()

	root := newRootForTest()
	root.SetArgs(append([]string{"wizard", "install"}, args...))
	root.SetIn(strings.NewReader(""))
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)

	err := root.Execute()
	if err != nil {
		return "", realDir, err
	}
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		return "", realDir, nil
	}
	return string(data), realDir, nil
}

// writeDummyScript creates a real file at dir/name and returns its path.
// Install requires the script to exist; the file is empty because we
// never actually execute the process in tests.
func writeDummyScript(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

func TestInstallFlagMutex(t *testing.T) {
	dir := t.TempDir()
	script := writeDummyScript(t, dir, "agy")
	_, _, err := runInstall(t, dir, []string{script, "x", "--system-planner", "--business-planner"})
	if err == nil {
		t.Fatal("expected mutex error, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error should mention mutually exclusive, got: %v", err)
	}
}

// TestInstallAcceptsMissingScript documents that `wizard install`
// does NOT pre-flight the script path — bare names like `claude` or
// `agy` are valid input because the daemon resolves them via PATH at
// launch time.
func TestInstallAcceptsMissingScript(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := runInstall(t, dir, []string{"/does/not/exist", "--system-planner"}); err != nil {
		t.Fatalf("install should not pre-flight the script, got: %v", err)
	}
	cfg, err := config.Load(filepath.Join(dir, "ecosystem.config.js"))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(cfg.Apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(cfg.Apps))
	}
	if cfg.Apps[0].Script != "/does/not/exist" {
		t.Errorf("Script = %q", cfg.Apps[0].Script)
	}
}

func TestInstallEndToEnd(t *testing.T) {
	dir := t.TempDir()
	script := writeDummyScript(t, dir, "agy")

	got, realDir, err := runInstall(t, dir, []string{script, "analyze repo", "--system-planner"})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	wantName := `name: "agy-` + filepath.Base(dir) + `"`
	if !strings.Contains(got, wantName) {
		t.Errorf("missing %q:\n%s", wantName, got)
	}
	if !strings.Contains(got, `namespace: "planner"`) {
		t.Errorf("missing planner namespace:\n%s", got)
	}
	// CWD on macOS may resolve /var → /private/var, so we use
	// the real cwd after chdir instead of the raw dir argument.
	if realDir == "" {
		realDir = dir
	}
	wantArgsLine := `args: ["--add-dir", ` + strconvQuote(realDir) + `, "-p", ` +
		strconvQuote("'"+ecoPlannerSystemPrefix+" analyze repo'") + `]`
	if !strings.Contains(got, wantArgsLine) {
		t.Errorf("args line not as expected, want %s:\n%s", wantArgsLine, got)
	}
	if !strings.Contains(got, `cwd: "`+realDir+`"`) {
		t.Errorf("missing cwd line, want %q:\n%s", realDir, got)
	}
	// Round-trip through config.Load to confirm parsability.
	cfg, err := config.Load(filepath.Join(dir, "ecosystem.config.js"))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(cfg.Apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(cfg.Apps))
	}
	a := cfg.Apps[0]
	if a.Name != "agy-"+filepath.Base(dir) {
		t.Errorf("loaded Name = %q", a.Name)
	}
	if a.Namespace != ecoPlannerNS {
		t.Errorf("loaded Namespace = %q, want %q", a.Namespace, ecoPlannerNS)
	}
	wantLoadedArgs := []string{"--add-dir", realDir, "-p", "'" + ecoPlannerSystemPrefix + " analyze repo'"}
	if len(a.Args) != len(wantLoadedArgs) {
		t.Fatalf("loaded len(Args) = %d, want %d (%v)", len(a.Args), len(wantLoadedArgs), a.Args)
	}
	for i, w := range wantLoadedArgs {
		if a.Args[i] != w {
			t.Errorf("loaded Args[%d] = %q, want %q", i, a.Args[i], w)
		}
	}
	if a.CWD != realDir {
		t.Errorf("loaded CWD = %q, want %q", a.CWD, realDir)
	}
}

func TestInstallNoUserPrompt(t *testing.T) {
	dir := t.TempDir()
	script := writeDummyScript(t, dir, "agy")

	if _, _, err := runInstall(t, dir, []string{script, "--system-planner"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	cfg, err := config.Load(filepath.Join(dir, "ecosystem.config.js"))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(cfg.Apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(cfg.Apps))
	}
	a := cfg.Apps[0]
	if a.Name != "agy-"+filepath.Base(dir) {
		t.Errorf("loaded Name = %q", a.Name)
	}
	if a.Namespace != ecoPlannerNS {
		t.Errorf("loaded Namespace = %q, want %q", a.Namespace, ecoPlannerNS)
	}
	realDir, _ := os.Getwd()
	if realDir == "" {
		realDir = dir
	}
	// No user_prompt → -p value is the bare single-quoted prefix.
	wantArgs := []string{"--add-dir", realDir, "-p", "'" + ecoPlannerSystemPrefix + "'"}
	if len(a.Args) != len(wantArgs) {
		t.Fatalf("expected %d args, got %d (%v)", len(wantArgs), len(a.Args), a.Args)
	}
	for i, w := range wantArgs {
		if a.Args[i] != w {
			t.Errorf("Args[%d] = %q, want %q", i, a.Args[i], w)
		}
	}
	if a.CWD != realDir {
		t.Errorf("loaded CWD = %q, want %q", a.CWD, realDir)
	}
}

func TestInstallMergesIntoExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ecosystem.config.js")
	seed := `module.exports = { apps: [ { name: "api", script: "./a.js" } ] };`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	script := writeDummyScript(t, dir, "agy")
	var err error
	if _, _, err = runInstall(t, dir, []string{script, "do X", "--system-planner", "--output", path}); err != nil {
		t.Fatalf("install: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(cfg.Apps) != 2 {
		t.Fatalf("expected 2 apps after merge, got %d", len(cfg.Apps))
	}
}

// strconvQuote is a tiny wrapper to keep test expectations readable.
func strconvQuote(s string) string { return fmt.Sprintf("%q", s) }
