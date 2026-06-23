package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bizshuk/pm2/config"
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
