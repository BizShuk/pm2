package wizard

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
	"github.com/bizshuk/pm2/process"
)

// ---------- renderEcosystemJS ----------

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
	app := process.AppConfig{
		Name:      "api",
		Script:    "./bin/server",
		Args:      []string{"--port", "8080"},
		Namespace: "default",
		Instances: 2,
		Env:       map[string]string{"NODE_ENV": "production"},
	}
	app.Normalize("")

	got := renderEcosystemJS([]process.AppConfig{app})

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
	app := process.AppConfig{
		Name:   "minimal",
		Script: "app.js",
	}
	app.Normalize("")

	got := renderEcosystemJS([]process.AppConfig{app})

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

// ---------- renderEcosystemJSON ----------

func TestRenderEcosystemJSON(t *testing.T) {
	app := process.AppConfig{
		Name:      "worker",
		Script:    "worker.js",
		Args:      []string{"--q"},
		Namespace: "jobs",
		Instances: 3,
		Watch:     true,
		Env:       map[string]string{"FOO": "bar"},
	}
	app.Normalize("")

	out, err := renderEcosystemJSON([]process.AppConfig{app})
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

// ---------- render round trip ----------

func TestRenderRoundTrip(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pm2-test")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cases := []struct {
		name string
		apps []process.AppConfig
	}{
		{
			name: "single",
			apps: []process.AppConfig{
				{Name: "api", Script: "./server.js", Instances: 1, Namespace: "default"},
			},
		},
		{
			name: "multi",
			apps: []process.AppConfig{
				{Name: "api", Script: "./a.js", Instances: 2, Namespace: "default", Watch: true},
				{Name: "worker", Script: "./b.js", Instances: 1, Namespace: "jobs",
					Env: map[string]string{"K": "V"}},
			},
		},
		{
			name: "cron",
			apps: []process.AppConfig{
				{Name: "sched", Script: "./c.js", Cron: "0 * * * *", CronRestart: "0 3 * * *"},
			},
		},
	}

	for _, tc := range cases {
		// Normalize inputs to match what the loader will produce.
		for i := range tc.apps {
			tc.apps[i].Normalize("")
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
	app := process.AppConfig{
		Name:   "escape-test",
		Script: weird,
	}
	app.Normalize("")

	tempDir, err := os.MkdirTemp("", "pm2-test")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	js := renderEcosystemJS([]process.AppConfig{app})
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

// ---------- collectAnswers ----------

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

// ---------- promptYesNo ----------

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

// ---------- deriveName ----------

func TestDeriveName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"app.js", "app"},
		{"./bin/server", "server"},
		{"/abs/path/worker.ts", "worker"},
		{"noext", "noext"},
		{"", defaultName},
	}
	for _, c := range cases {
		if got := DeriveName(c.in); got != c.want {
			t.Errorf("DeriveName(%q) = %q, want %q", c.in, got, c.want)
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

func mkApp(name, script string) process.AppConfig {
	a := process.AppConfig{Name: name, Script: script}
	a.Normalize("")
	return a
}

func TestMergeAppsByNameNoCollision(t *testing.T) {
	existing := []process.AppConfig{mkApp("api", "a.js"), mkApp("worker", "w.js")}
	newApps := []process.AppConfig{mkApp("cron", "c.js")}
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
	existing := []process.AppConfig{
		{Name: "api", Script: "a.js", Instances: 4, Env: map[string]string{"K": "v"}},
		mkApp("worker", "w.js"),
	}
	newApps := []process.AppConfig{
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
	existing := []process.AppConfig{mkApp("api", "a.js")}
	newApps := []process.AppConfig{mkApp("api", "a2.js"), mkApp("api", "a3.js")}
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
		{"eco.js", FormatJS, true},
		{"eco.cjs", FormatJS, true},
		{"eco.mjs", FormatJS, true},
		{"eco.json", FormatJSON, true},
		{"eco.yaml", "", false},
		{"eco", "", false},
		{"/abs/path/ECO.JSON", FormatJSON, true},
	}
	for _, c := range cases {
		gotFmt, gotOK := detectFormatFromExt(c.path)
		if gotFmt != c.wantFmt || gotOK != c.wantOK {
			t.Errorf("detectFormatFromExt(%q) = (%q,%v), want (%q,%v)",
				c.path, gotFmt, gotOK, c.wantFmt, c.wantOK)
		}
	}
}

// ---------- public API: RunInteractive (new tests at the public surface) ----------

// TestRunInteractiveYesAllSynthesizesDefaultApp drives the non-interactive
// (--yes) flow through the public entry point and confirms a single
// default app lands in the output file with no prompts.
func TestRunInteractiveYesAllSynthesizesDefaultApp(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "ecosystem.config.js")

	ctx := WizardContext{
		In:     strings.NewReader(""),
		Out:    &bytes.Buffer{},
		ErrOut: &bytes.Buffer{},
		YesAll: true,
	}
	if err := RunInteractive(ctx, RunOptions{Output: out}); err != nil {
		t.Fatalf("RunInteractive: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(data), `name: "app"`) {
		t.Errorf("expected default app in output, got:\n%s", data)
	}
}

// TestRunInteractiveRejectsBadFormat ensures the format validator fires
// before any I/O happens.
func TestRunInteractiveRejectsBadFormat(t *testing.T) {
	ctx := WizardContext{In: strings.NewReader(""), Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}
	err := RunInteractive(ctx, RunOptions{Format: "yaml"})
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "invalid --format") {
		t.Errorf("error should mention --format, got: %v", err)
	}
}

// TestRunInstallForcesNonInteractive confirms RunInstall never blocks
// on the "Write?" prompt even when ctx.In is a closed reader.
func TestRunInstallForcesNonInteractive(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "ecosystem.config.js")

	ctx := WizardContext{
		In:     strings.NewReader(""), // would block on promptYesNo
		Out:    &bytes.Buffer{},
		ErrOut: &bytes.Buffer{},
		// YesAll intentionally false — RunInstall must override.
		YesAll: false,
	}
	app := DefaultApp()
	if err := RunInstall(ctx, app, InstallOptions{Output: out}); err != nil {
		t.Fatalf("RunInstall: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(data), `name: "app"`) {
		t.Errorf("expected default app in output, got:\n%s", data)
	}
}

// TestWriteEcosystemFilePreviewGoesToErrOut documents the channel
// contract: the preview (and merge summary) are written to ctx.ErrOut,
// the "Wrote <abs>" confirmation goes to ctx.Out. This is how the CLI
// shell can route them to stderr/stdout independently.
func TestWriteEcosystemFilePreviewGoesToErrOut(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "ecosystem.config.js")

	var stdout, stderr bytes.Buffer
	ctx := WizardContext{
		In:     strings.NewReader(""),
		Out:    &stdout,
		ErrOut: &stderr,
		YesAll: true,
	}
	if err := WriteEcosystemFile(ctx, []process.AppConfig{DefaultApp()}, out, DefaultWriteOptions()); err != nil {
		t.Fatalf("WriteEcosystemFile: %v", err)
	}
	if !strings.Contains(stderr.String(), "preview of") {
		t.Errorf("expected preview in ErrOut, got:\n%s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Wrote ") {
		t.Errorf("expected Wrote line in Out, got:\n%s", stdout.String())
	}
}

// TestWriteEcosystemFileRefusesOverwriteWithoutForce documents the
// --no-merge safety net: when the file exists and Force is false the
// merge path runs (no error); when NoMerge is set the call aborts.
func TestWriteEcosystemFileNoMergeAborts(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "ecosystem.config.js")
	seed := `module.exports = { apps: [ { name: "api", script: "./a.js" } ] };`
	if err := os.WriteFile(out, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ctx := WizardContext{In: strings.NewReader(""), Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}, YesAll: true}
	err := WriteEcosystemFile(ctx, []process.AppConfig{DefaultApp()}, out, WriteOptions{NoMerge: true})
	if err == nil {
		t.Fatal("expected abort with --no-merge on existing file")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should mention --force, got: %v", err)
	}
}

// TestWriteEcosystemFileMalformedExistingSurfacesForceHint ensures the
// parse-error branch wraps with the "(use --force ...)" hint.
func TestWriteEcosystemFileMalformedExistingSurfacesForceHint(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "ecosystem.config.js")
	if err := os.WriteFile(out, []byte("module.exports = { apps: ["), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ctx := WizardContext{In: strings.NewReader(""), Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}, YesAll: true}
	err := WriteEcosystemFile(ctx, []process.AppConfig{DefaultApp()}, out, DefaultWriteOptions())
	if err == nil {
		t.Fatal("expected abort on malformed existing file")
	}
	if !strings.Contains(err.Error(), "use --force") {
		t.Errorf("error should mention --force, got: %v", err)
	}
}

// strconvQuote is a tiny wrapper to keep test expectations readable.
func strconvQuote(s string) string { return fmt.Sprintf("%q", s) }