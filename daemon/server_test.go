package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bizshuk/pm2/process"
)

// TestBaseEnvSnapshotReachesProcess verifies that an env var present only in
// req.BaseEnv (the CLI snapshot) — and absent from the daemon's own
// environment — is passed through to the spawned process.
func TestBaseEnvSnapshotReachesProcess(t *testing.T) {
	testDir := "/tmp/pm2-test-baseenv"
	_ = os.RemoveAll(testDir)
	_ = os.MkdirAll(testDir, 0o755)
	s := NewServer(testDir)
	defer os.RemoveAll(testDir)

	const marker = "PM2_BASEENV_MARKER"
	const want = "from_cli_snapshot"
	if _, ok := os.LookupEnv(marker); ok {
		t.Fatalf("%s must not be set in the daemon/test environment", marker)
	}
	outPath := filepath.Join(testDir, "env.out")

	req := &AppStartReq{
		Namespace: "default",
		Name:      "envcheck",
		Script:    "/bin/sh",
		Args:      []string{"-c", "printenv " + marker + " > " + outPath},
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
	testDir := "/tmp/pm2-test-baseenv-persist"
	_ = os.RemoveAll(testDir)
	_ = os.MkdirAll(testDir, 0o755)
	defer os.RemoveAll(testDir)

	const marker = "PM2_BASEENV_PERSIST"
	const want = "snapshot_value"
	snapshot := append(os.Environ(), marker+"="+want)

	s := NewServer(testDir)
	req := &AppStartReq{
		Namespace: "default",
		Name:      "persistcheck",
		Script:    "/bin/sh",
		Args:      []string{"-c", "sleep 30"},
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
	s := NewServer("/tmp/pm2-test")
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
	testDir := "/tmp/pm2-test-watch"
	_ = os.RemoveAll(testDir)
	_ = os.MkdirAll(testDir, 0o755)
	s := NewServer(testDir)
	defer os.RemoveAll(testDir)

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
	testDir := "/tmp/pm2-test-version"
	_ = os.RemoveAll(testDir)
	_ = os.MkdirAll(testDir, 0o755)
	s := NewServer(testDir)
	defer os.RemoveAll(testDir)

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

// TestKillAllStopsEveryProcess verifies the kill command's core: all managed
// processes are stopped and their PIDs cleared.
func TestKillAllStopsEveryProcess(t *testing.T) {
	testDir := "/tmp/pm2-test-killall"
	_ = os.RemoveAll(testDir)
	_ = os.MkdirAll(testDir, 0o755)
	s := NewServer(testDir)
	defer os.RemoveAll(testDir)

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
	testDir := "/tmp/pm2-test-configfile"
	_ = os.RemoveAll(testDir)
	_ = os.MkdirAll(testDir, 0o755)
	s := NewServer(testDir)
	defer os.RemoveAll(testDir)

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
	testDir := "/tmp/pm2-test-deleterestart"
	_ = os.RemoveAll(testDir)
	_ = os.MkdirAll(testDir, 0o755)
	s := NewServer(testDir)
	s.RestartDelay = 500 * time.Millisecond
	defer os.RemoveAll(testDir)

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
	testDir := "/tmp/pm2-test-restartsinherit"
	_ = os.RemoveAll(testDir)
	_ = os.MkdirAll(testDir, 0o755)
	s := NewServer(testDir)
	defer os.RemoveAll(testDir)

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



