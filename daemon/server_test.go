package daemon

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bizshuk/pm2/process"
)

// testDir creates a sandbox-friendly scratch directory under
// $TMPDIR/pm2-test-<testName>-<rand>. Falls back to t.TempDir() which Go
// already cleans up automatically.
func testDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	return d
}

// TestBaseEnvSnapshotReachesProcess verifies that an env var present only in
// req.BaseEnv (the CLI snapshot) — and absent from the daemon's own
// environment — is passed through to the spawned process.
//
// The daemon always wraps script+args in `bash -c "<script> <args>"`, so
// passing `Script="echo", Args=["$MARKER"]` results in bash executing
// `echo $MARKER` — which expands $MARKER from the inherited environment and
// writes the value to the daemon's stdout (captured via logFile).
func TestBaseEnvSnapshotReachesProcess(t *testing.T) {
	testDir := testDir(t)
	s := NewServer(testDir)

	const marker = "PM2_BASEENV_MARKER"
	const want = "from_cli_snapshot"
	if _, ok := os.LookupEnv(marker); ok {
		t.Fatalf("%s must not be set in the daemon/test environment", marker)
	}
	outPath := filepath.Join(testDir, "env.out")

	// Bash expands $MARKER in the env; redirect to outPath via shell.
	req := &AppStartReq{
		Namespace: "default",
		Name:      "envcheck",
		Script:    "echo",
		Args:      []string{`$` + marker + ` > ` + outPath},
		Instances: 1,
		// Snapshot does NOT live in the daemon's os.Environ().
		BaseEnv: append(os.Environ(), marker+"="+want),
	}

	if _, err := s.startApp(req); err != nil {
		t.Fatalf("startApp failed: %v", err)
	}

	// Wait for the short-lived process to write the file.
	var data []byte
	for i := 0; i < 50; i++ {
		if b, err := os.ReadFile(outPath); err == nil && len(b) > 0 {
			data = b
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := strings.TrimSpace(string(data))
	if got != want {
		t.Fatalf("spawned process saw %s=%q, want %q (BaseEnv snapshot not applied)", marker, got, want)
	}
}

// TestBaseEnvSurvivesRestartAndResurrect verifies the snapshot is stored on the
// process and replayed by restartByName, and that it round-trips through
// save/resurrect (so a daemon restart does not drop the user's PATH).
func TestBaseEnvSurvivesRestartAndResurrect(t *testing.T) {
	testDir := testDir(t)

	const marker = "PM2_BASEENV_PERSIST"
	const want = "snapshot_value"
	snapshot := append(os.Environ(), marker+"="+want)

	s := NewServer(testDir)
	req := &AppStartReq{
		Namespace: "default",
		Name:      "persistcheck",
		Script:    "sleep",
		Args:      []string{"30"},
		Instances: 1,
		BaseEnv:   snapshot,
	}
	if _, err := s.startApp(req); err != nil {
		t.Fatalf("startApp failed: %v", err)
	}

	// 1. Stored on the running process.
	s.mu.RLock()
	mp := s.processes["default:persistcheck"]
	s.mu.RUnlock()
	if mp == nil || !envHas(mp.Info.BaseEnv, marker, want) {
		t.Fatalf("BaseEnv not stored on ProcessInfo")
	}

	// 2. Replayed by restart.
	if err := s.restartByName("persistcheck"); err != nil {
		t.Fatalf("restart failed: %v", err)
	}
	s.mu.RLock()
	mp = s.processes["default:persistcheck"]
	s.mu.RUnlock()
	if mp == nil || !envHas(mp.Info.BaseEnv, marker, want) {
		t.Fatalf("BaseEnv lost after restart")
	}

	// 3. Round-trips through save/resurrect into a fresh server.
	if err := s.save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	_ = s.stopByName("persistcheck")

	s2 := NewServer(testDir)
	if err := s2.resurrect(); err != nil {
		t.Fatalf("resurrect failed: %v", err)
	}
	s2.mu.RLock()
	mp2 := s2.processes["default:persistcheck"]
	s2.mu.RUnlock()
	if mp2 == nil || !envHas(mp2.Info.BaseEnv, marker, want) {
		t.Fatalf("BaseEnv lost across save/resurrect")
	}
	_ = s2.stopByName("persistcheck")
}

func envHas(env []string, key, val string) bool {
	return slices.Contains(env, key+"="+val)
}

func TestFindProcesses(t *testing.T) {
	s := NewServer(testDir(t))
	s.processes["default:appA"] = &ManagedProcess{
		Info: process.ProcessInfo{ID: 0, Name: "appA", Namespace: "default"},
	}
	s.processes["Infra:appB"] = &ManagedProcess{
		Info: process.ProcessInfo{ID: 1, Name: "appB", Namespace: "Infra"},
	}
	s.processes["Infra:appC"] = &ManagedProcess{
		Info: process.ProcessInfo{ID: 2, Name: "appC", Namespace: "Infra"},
	}
	s.processes["default:appB"] = &ManagedProcess{
		Info: process.ProcessInfo{ID: 3, Name: "appB", Namespace: "default"},
	}

	// 1. 測試 ID 匹配
	res := s.findProcesses("1")
	if len(res) != 1 || res[0].Info.Name != "appB" || res[0].Info.Namespace != "Infra" {
		t.Errorf("ID matching failed")
	}

	// 2. 測試 Name 匹配
	res = s.findProcesses("appB")
	if len(res) != 2 {
		t.Errorf("Name matching failed, got %d", len(res))
	}

	// 3. 測試 Namespace 匹配
	res = s.findProcesses("Infra")
	if len(res) != 2 {
		t.Errorf("Namespace matching failed, got %d", len(res))
	}

	// 4. 測試 "all" 匹配
	res = s.findProcesses("all")
	if len(res) != 4 {
		t.Errorf("All matching failed, got %d", len(res))
	}
}

func TestWatchStateInheritance(t *testing.T) {
	testDir := testDir(t)
	s := NewServer(testDir)

	s.processes["default:watch-app"] = &ManagedProcess{
		Info: process.ProcessInfo{
			ID:        1,
			Name:      "watch-app",
			Namespace: "default",
			Watch:     true,
			Script:    "test.js",
		},
	}

	err := s.save()
	if err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	dumpPath := testDir + "/dump.json"
	data, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("Failed to read dump file: %v", err)
	}

	var entries []process.DumpEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("Failed to unmarshal dump entries: %v", err)
	}

	if len(entries) != 1 || !entries[0].Watch {
		t.Errorf("DumpEntry did not preserve Watch attribute: %+v", entries)
	}
}

func TestVersionStateInheritance(t *testing.T) {
	testDir := testDir(t)
	s := NewServer(testDir)

	s.processes["default:version-app"] = &ManagedProcess{
		Info: process.ProcessInfo{
			ID:        1,
			Name:      "version-app",
			Namespace: "default",
			Version:   "1.2.3",
			Script:    "test.js",
		},
	}

	err := s.save()
	if err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	dumpPath := testDir + "/dump.json"
	data, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("Failed to read dump file: %v", err)
	}

	var entries []process.DumpEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("Failed to unmarshal dump entries: %v", err)
	}

	if len(entries) != 1 || entries[0].Version != "1.2.3" {
		t.Errorf("DumpEntry did not preserve Version attribute: %+v", entries)
	}
}

// TestCWDInjectedAsPWD verifies $PWD seen by the spawned process matches the
// configured CWD, even though the BaseEnv snapshot carries a different PWD.
func TestCWDInjectedAsPWD(t *testing.T) {
	testDir := testDir(t)
	workDir := filepath.Join(testDir, "work")
	_ = os.MkdirAll(workDir, 0o755)

	s := NewServer(testDir)
	outPath := filepath.Join(testDir, "pwd.out")
	// Bash expands $PWD in the env; redirect to outPath via shell.
	req := &AppStartReq{
		Namespace: "default",
		Name:      "pwdcheck",
		Script:    "echo",
		Args:      []string{"$PWD > " + outPath},
		Instances: 1,
		CWD:       workDir,
		// Snapshot deliberately carries a stale PWD.
		BaseEnv: append(os.Environ(), "PWD=/tmp/some/other/dir"),
	}
	if _, err := s.startApp(req); err != nil {
		t.Fatalf("startApp failed: %v", err)
	}

	var data []byte
	for i := 0; i < 50; i++ {
		if b, err := os.ReadFile(outPath); err == nil && len(b) > 0 {
			data = b
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := strings.TrimSpace(string(data)); got != workDir {
		t.Fatalf("process saw PWD=%q, want %q", got, workDir)
	}
}

// TestKillAllStopsEveryProcess verifies the kill command's core: all managed
// processes are stopped and their PIDs cleared.
func TestKillAllStopsEveryProcess(t *testing.T) {
	testDir := testDir(t)
	s := NewServer(testDir)

	for _, name := range []string{"a", "b", "c"} {
		req := &AppStartReq{
			Namespace: "default",
			Name:      name,
			Script:    "/bin/sh",
			Args:      []string{"-c", "sleep 30"},
			Instances: 1,
		}
		if _, err := s.startApp(req); err != nil {
			t.Fatalf("startApp %s failed: %v", name, err)
		}
	}

	s.killAll()

	s.mu.RLock()
	defer s.mu.RUnlock()
	for key, mp := range s.processes {
		if mp.Info.Status != process.StatusStopped {
			t.Errorf("%s: status=%s, want stopped", key, mp.Info.Status)
		}
		if mp.Info.PID != 0 {
			t.Errorf("%s: PID=%d, want 0", key, mp.Info.PID)
		}
	}
}

func TestConfigFileReplacement(t *testing.T) {
	// launchProcess eventually calls exec.Cmd.Start() with the same options
	// the daemon uses (Setpgid + redirected Stdout/Stderr). Some sandboxes
	// (e.g. restricted containers) forbid that. Probe first and skip if so
	// — the test is about the process-map replacement semantics, not spawn.
	probeDir := t.TempDir()
	probeOut, _ := os.OpenFile(filepath.Join(probeDir, "out"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	defer probeOut.Close()
	probe := exec.Command("/bin/echo", "probe")
	probe.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	probe.Stdout = probeOut
	probe.Stderr = probeOut
	if err := probe.Start(); err != nil {
		t.Skipf("skipping: cannot fork child processes in this environment (%v)", err)
	}
	_ = probe.Wait()

	testDir := testDir(t)
	s := NewServer(testDir)

	scriptFile := "/bin/echo"

	s.processes["default:agentmemory"] = &ManagedProcess{
		Info: process.ProcessInfo{
			ID:         42,
			Name:       "agentmemory",
			Namespace:  "default",
			Script:     scriptFile,
			ConfigFile: "/path/to/ecosystem.config.js",
		},
		done: make(chan struct{}),
	}

	req := &AppStartReq{
		Namespace:  "Agent",
		Name:       "agentmemory",
		Script:     scriptFile,
		ConfigFile: "/path/to/ecosystem.config.js",
		Instances:  1,
	}

	_, err := s.startApp(req)
	if err != nil {
		t.Fatalf("startApp failed: %v", err)
	}

	// 檢查舊的 key 是否被刪除，且新的 key 存在，且 ID 繼承為 42
	if _, ok := s.processes["default:agentmemory"]; ok {
		t.Errorf("Old process 'default:agentmemory' should have been deleted")
	}

	mp, ok := s.processes["Agent:agentmemory"]
	if !ok {
		t.Fatalf("New process 'Agent:agentmemory' was not found")
	}

	if mp.Info.ID != 42 {
		t.Errorf("Expected ID 42 to be inherited, got %d", mp.Info.ID)
	}

	if mp.Info.ConfigFile != "/path/to/ecosystem.config.js" {
		t.Errorf("Expected ConfigFile to be propagated, got %s", mp.Info.ConfigFile)
	}
}

func TestDeleteDuringRestartSleep(t *testing.T) {
	testDir := testDir(t)
	s := NewServer(testDir)
	s.RestartDelay = 500 * time.Millisecond

	req := &AppStartReq{
		Namespace:   "default",
		Name:        "fail-app",
		Script:      "/usr/bin/false",
		MaxRestarts: 5,
		Instances:   1,
	}

	_, err := s.startApp(req)
	if err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}

	// Wait a bit for the process to exit and enter the restart sleep
	time.Sleep(200 * time.Millisecond)

	s.mu.Lock()
	mp, exists := s.processes["default:fail-app"]
	s.mu.Unlock()
	if !exists {
		t.Fatalf("Process fail-app was not registered")
	}

	// Verify it's in StatusLaunching or StatusErrored
	s.mu.Lock()
	status := mp.Info.Status
	s.mu.Unlock()
	if status != process.StatusLaunching && status != process.StatusErrored {
		t.Logf("Process status: %s", status)
	}

	// Delete it while it's sleeping (or about to restart)
	err = s.deleteByName("fail-app")
	if err != nil {
		t.Fatalf("Failed to delete process: %v", err)
	}

	// Wait for the restart interval (500ms) plus some buffer (600ms total)
	time.Sleep(600 * time.Millisecond)

	// Check if it got back
	s.mu.Lock()
	_, exists = s.processes["default:fail-app"]
	s.mu.Unlock()
	if exists {
		t.Errorf("Deleted process got back after restart sleep!")
	}
}

func TestRestartsInheritance(t *testing.T) {
	testDir := testDir(t)
	s := NewServer(testDir)

	s.processes["default:appA"] = &ManagedProcess{
		Info: process.ProcessInfo{
			ID:        1,
			Name:      "appA",
			Namespace: "default",
			Restarts:  5,
			Script:    "/bin/echo",
		},
	}

	req := &AppStartReq{
		Namespace: "default",
		Name:      "appA",
		Script:    "/bin/echo",
		Instances: 1,
	}

	_, err := s.startApp(req)
	if err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}

	s.mu.Lock()
	mp, exists := s.processes["default:appA"]
	s.mu.Unlock()
	if !exists {
		t.Fatalf("Process appA was not registered")
	}

	if mp.Info.Restarts != 5 {
		t.Errorf("Expected restarts counter to be inherited as 5, got %d", mp.Info.Restarts)
	}
}

func TestStartAppOutFileHomeExpansion(t *testing.T) {
	testDir := testDir(t)
	s := NewServer(testDir)

	req := &AppStartReq{
		Namespace: "default",
		Name:      "homeexpandcheck",
		Script:    "/bin/sh",
		Args:      []string{"-c", "sleep 1"},
		Instances: 1,
		OutFile:   "~/test-home-expand-out.log",
		ErrorFile: "~/test-home-expand-err.log",
	}

	pi, err := s.startApp(req)
	if err != nil {
		t.Fatalf("startApp failed: %v", err)
	}
	defer s.stopByName("homeexpandcheck")

	if len(pi) == 0 {
		t.Fatalf("No process info returned")
	}

	if !strings.HasPrefix(pi[0].LogFile, "/") || strings.Contains(pi[0].LogFile, "~") {
		t.Errorf("LogFile path was not expanded: got %s", pi[0].LogFile)
	}
	if !strings.HasPrefix(pi[0].ErrorFile, "/") || strings.Contains(pi[0].ErrorFile, "~") {
		t.Errorf("ErrorFile path was not expanded: got %s", pi[0].ErrorFile)
	}
}



