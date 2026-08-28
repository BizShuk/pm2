package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wizardcmd "github.com/bizshuk/pm2/cmd/wizard"
	plannerprompt "github.com/bizshuk/pm2/cmd/wizard/prompt"
	"github.com/bizshuk/pm2/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

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
	resetCommandForTest(t, wizardcmd.InstallCmd)

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

func TestWizardPromptCopy(t *testing.T) {
	for _, want := range []string{
		"Prompts in order: namespace, name, script, args, instances, watch mode, env, cron schedule, cron restart, max restarts, cwd, optional, add another app, then an optional workflows block, then write to file.",
		"each stage is a shell script, a registered task run once, or another workflow.",
		"Namespace choices: Agent, Backup, Local, Service, AutoP.",
		"Generated names use uppercase NAMESPACE SCRIPT - NAME.",
		"Enter r for a random daily time between 2am and 8am.",
		"Defaults: max restarts 15; cwd uses the ecosystem file directory; config and log paths follow PM2 defaults.",
		"Optional choice 1 registers the app paused.",
	} {
		if !strings.Contains(WizardCmd.Long, want) {
			t.Errorf("wizard long help missing %q:\n%s", want, WizardCmd.Long)
		}
	}

	previousTTY := isTerminalFunc
	isTerminalFunc = func(fd uintptr) bool { return false }
	t.Cleanup(func() { isTerminalFunc = previousTTY })

	root := newRootForTest(t)
	root.SetArgs([]string{"wizard"})
	root.SetIn(strings.NewReader(""))
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	if err := root.Execute(); err == nil {
		t.Fatal("wizard without a terminal or --yes returned nil error")
	}
	if !strings.Contains(stderr.String(), "pm2 wizard requires an interactive terminal") {
		t.Errorf("non-terminal prompt does not name pm2 wizard:\n%s", stderr.String())
	}
	if strings.Contains(stderr.String(), "pm2 eco") {
		t.Errorf("non-terminal prompt retains stale pm2 eco name:\n%s", stderr.String())
	}
}

func TestWizardFinalWritePrompt(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "ecosystem.config.js")

	previousTTY := isTerminalFunc
	isTerminalFunc = func(fd uintptr) bool { return true }
	t.Cleanup(func() { isTerminalFunc = previousTTY })

	root := newRootForTest(t)
	root.SetArgs([]string{"wizard", "--output", output})
	root.SetIn(strings.NewReader(
		strings.Repeat("\n", 12) +
			"n\n" + // add another app
			"n\n" + // add a workflow
			"n\n", // write to file
	))
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	if err := root.Execute(); err != nil {
		t.Fatalf("wizard: %v\nstderr:\n%s", err, stderr.String())
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("output file exists after declining final write: err=%v", err)
	}
	if !strings.Contains(stdout.String(), "Write to file "+output+"? [Y/n]: ") {
		t.Errorf("missing write-to-file prompt:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Aborted.") {
		t.Errorf("missing aborted confirmation:\n%s", stdout.String())
	}
}

func TestWizardEndToEndMerge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ecosystem.config.js")
	seed := `module.exports = { apps: [ { name: "api", script: "./a.js" } ] };`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// askOneApp prompt sequence, then stop app collection and write:
	//   namespace, name, script, args, instances, watch, env-blank,
	//   cron-blank, cron restart, max restarts, cwd, optional → "n" → "y"
	stdin := strings.Join([]string{
		"3",         // namespace → Local
		"worker",    // name
		"worker.js", // script
		"",          // args
		"",          // instances
		"",          // watch
		"",          // env
		"",          // cron
		"",          // cron restart
		"",          // max restarts
		"",          // cwd
		"2",         // optional → no
		"n",         // add another? → no
		"n",         // add a workflow? → no
		"y",         // write to file → yes
	}, "\n") + "\n"

	got, err := runWizard(t, dir, stdin,
		"--output "+path)
	if err != nil {
		t.Fatalf("wizard: %v", err)
	}
	if !strings.Contains(got, `name: "api"`) {
		t.Errorf("merged output missing 'api':\n%s", got)
	}
	if !strings.Contains(got, `name: "LOCAL WORKER - WORKER"`) {
		t.Errorf("merged output missing composed worker name:\n%s", got)
	}
	if !strings.Contains(got, `namespace: "Local"`) {
		t.Errorf("merged output missing Local namespace:\n%s", got)
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
	if !strings.Contains(got, `name: "AGENT APP - APP"`) {
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
	if !strings.Contains(got, `name: "AGENT APP - APP"`) {
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
		strconvQuote("'"+plannerprompt.System().Render("analyze repo")+"'") + `]`
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
	if a.Namespace != wizardcmd.EcoPlannerNS {
		t.Errorf("loaded Namespace = %q, want %q", a.Namespace, wizardcmd.EcoPlannerNS)
	}
	wantLoadedArgs := []string{
		"--add-dir",
		realDir,
		"-p",
		"'" + plannerprompt.System().Render("analyze repo") + "'",
	}
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
	if a.Namespace != wizardcmd.EcoPlannerNS {
		t.Errorf("loaded Namespace = %q, want %q", a.Namespace, wizardcmd.EcoPlannerNS)
	}
	realDir, _ := os.Getwd()
	if realDir == "" {
		realDir = dir
	}
	// No user_prompt → -p value is the bare single-quoted prefix.
	wantArgs := []string{
		"--add-dir",
		realDir,
		"-p",
		"'" + plannerprompt.System().Render("") + "'",
	}
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

// TestWizardEndToEndWorkflow drives the full CLI path — prompts to file
// to config.Load — for a file whose workflow references the app the same
// session declared. It is the check that the picker's key format and the
// loader's expectation agree.
func TestWizardEndToEndWorkflow(t *testing.T) {
	dir := t.TempDir()
	stdin := strings.Join([]string{
		"3",         // namespace → Local
		"worker",    // name
		"worker.js", // script
		"",          // args
		"",          // instances
		"",          // watch
		"",          // env
		"",          // cron
		"",          // cron restart
		"",          // max restarts
		"",          // cwd
		"2",         // optional → no
		"n",         // add another app? → no
		"y",         // add a workflow? → yes
		"ci",        // category
		"nightly",   // name
		"0 3 * * *", // cron
		"",          // timeout
		"",          // cwd
		"",          // env
		"1",         // stage type → script
		"unit",      // stage name
		"make",      // script
		"test",      // args
		"",          // stage env
		"",          // stage cwd
		"",          // stage timeout
		"y",         // add another stage? → yes
		"2",         // stage type → task
		"bounce",    // stage name
		"1",         // task → the worker declared above
		"",          // stage timeout
		"n",         // add another stage? → no
		"n",         // add another workflow? → no
		"y",         // write to file → yes
	}, "\n") + "\n"

	got, err := runWizard(t, dir, stdin,
		"--output "+filepath.Join(dir, "ecosystem.config.js"))
	if err != nil {
		t.Fatalf("wizard: %v", err)
	}
	if !strings.Contains(got, `workflows: [`) {
		t.Fatalf("generated file has no workflows block:\n%s", got)
	}

	cfg, err := config.Load(filepath.Join(dir, "ecosystem.config.js"))
	if err != nil {
		t.Fatalf("reload generated file: %v\n%s", err, got)
	}
	if len(cfg.Workflows) != 1 {
		t.Fatalf("expected 1 workflow, got %d\n%s", len(cfg.Workflows), got)
	}
	wf := cfg.Workflows[0]
	if wf.Key() != "ci:nightly" || wf.Cron != "0 3 * * *" {
		t.Errorf("workflow = %q cron %q, want ci:nightly at 0 3 * * *", wf.Key(), wf.Cron)
	}
	if len(wf.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d\n%s", len(wf.Stages), got)
	}
	if want := "Local:LOCAL WORKER - WORKER"; wf.Stages[1].Task != want {
		t.Errorf("task stage = %q, want %q", wf.Stages[1].Task, want)
	}
}
