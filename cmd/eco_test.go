package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bizshuk/pm2/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// This file holds CLI-level integration tests only. Unit tests for
// the wizard's prompt / render / merge helpers live in
// config/wizard/wizard_test.go (see plans/architecture-wizard-decoupling.md).

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

func TestIsPlannerAgent(t *testing.T) {
	tests := []struct {
		script string
		want   bool
	}{
		{"agy", true},
		{"claude", true},
		{"claudem", true},
		{"claudew", true},
		{"/usr/local/bin/claudem", true},
		{"/usr/local/bin/claudew", true},
		{"node", false},
		{"python", false},
	}
	for _, tc := range tests {
		if got := isPlannerAgent(tc.script); got != tc.want {
			t.Errorf("isPlannerAgent(%q) = %t, want %t", tc.script, got, tc.want)
		}
	}
}

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
	root := newRootForTest(t)
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
func newRootForTest(t *testing.T) *cobra.Command {
	t.Helper()
	resetCommandForTest(t, WizardCmd)
	resetCommandForTest(t, WizardInstallCmd)

	root := &cobra.Command{Use: "pm2"}
	root.AddCommand(WizardCmd)
	return root
}

func resetCommandForTest(t *testing.T, command *cobra.Command) {
	t.Helper()
	command.Flags().VisitAll(func(flag *pflag.Flag) {
		if err := flag.Value.Set(flag.DefValue); err != nil {
			t.Fatalf("reset flag %s: %v", flag.Name, err)
		}
		flag.Changed = false
	})
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

	root := newRootForTest(t)
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
