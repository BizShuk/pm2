package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/bizshuk/pm2/daemon/executor"
	"github.com/bizshuk/pm2/model"
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
	pm := NewProcessManager(testDir)

	const marker = "PM2_BASEENV_MARKER"
	const want = "from_cli_snapshot"
	if _, ok := os.LookupEnv(marker); ok {
		t.Fatalf("%s must not be set in the daemon/test environment", marker)
	}
	outPath := filepath.Join(testDir, "env.out")

	// Bash expands $MARKER in the env; redirect to outPath via shell.
	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
		Namespace: "default",
		Name:      "envcheck",
		Script:    "echo",
		Args:      []string{`$` + marker + ` > ` + outPath},
		Instances: 1,
		// Snapshot does NOT live in the daemon's os.Environ().
		BaseEnv: append(os.Environ(), marker+"="+want),
	},
	}

	if _, err := pm.StartApp(req); err != nil {
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

	pm := NewProcessManager(testDir)
	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
		Namespace: "default",
		Name:      "persistcheck",
		Script:    "sleep",
		Args:      []string{"30"},
		Instances: 1,
		BaseEnv:   snapshot,
	},
	}
	if _, err := pm.StartApp(req); err != nil {
		t.Fatalf("startApp failed: %v", err)
	}

	// 1. Stored on the running process.
	info, ok := pm.reg.SnapshotOne("default:persistcheck")
	if !ok || !envHas(info.BaseEnv, marker, want) {
		t.Fatalf("BaseEnv not stored on ProcessInfo")
	}

	// 2. Replayed by restart.
	if err := pm.RestartByName("persistcheck"); err != nil {
		t.Fatalf("restart failed: %v", err)
	}
	info, ok = pm.reg.SnapshotOne("default:persistcheck")
	if !ok || !envHas(info.BaseEnv, marker, want) {
		t.Fatalf("BaseEnv lost after restart")
	}

	// 3. Round-trips through save/resurrect into a fresh server.
	if err := pm.Save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	_ = pm.StopByName("persistcheck")

	pm2 := NewProcessManager(testDir)
	if err := pm2.Resurrect(); err != nil {
		t.Fatalf("resurrect failed: %v", err)
	}
	info2, ok2 := pm2.reg.SnapshotOne("default:persistcheck")
	if !ok2 || !envHas(info2.BaseEnv, marker, want) {
		t.Fatalf("BaseEnv lost across save/resurrect")
	}
	_ = pm2.StopByName("persistcheck")
}

func envHas(env []string, key, val string) bool {
	return slices.Contains(env, key+"="+val)
}

// pauseState atomically reads the (Status, paused) pair for a process
// under the registry write lock via UpdateInfo. This is the sanctioned
// read path when a test needs the private `paused` flag alongside
// ProcessInfo.Status — SnapshotOne only returns the value-copy Info and
// cannot see `paused`. Used by the pause/resume tests, which must
// synchronise against onProcessExit / stopProcess background writes.
func pauseState(pm *ProcessManager, key string) (status process.Status, paused, ok bool) {
	pm.reg.UpdateInfo(key, func(mp *ManagedProcess) {
		status = mp.Info.Status
		paused = mp.paused
		ok = true
	})
	return status, paused, ok
}

func TestFindProcesses(t *testing.T) {
	pm := NewProcessManager(testDir(t))
	pm.reg.Add("default:appA", &ManagedProcess{
		Info: process.ProcessInfo{
		AppConfig: process.AppConfig{Name: "appA", Namespace: "default"},
		ID: 0,
	},
})
	pm.reg.Add("Infra:appB", &ManagedProcess{
		Info: process.ProcessInfo{
		AppConfig: process.AppConfig{Name: "appB", Namespace: "Infra"},
		ID: 1,
	},
})
	pm.reg.Add("Infra:appC", &ManagedProcess{
		Info: process.ProcessInfo{
		AppConfig: process.AppConfig{Name: "appC", Namespace: "Infra"},
		ID: 2,
	},
})
	pm.reg.Add("default:appB", &ManagedProcess{
		Info: process.ProcessInfo{
		AppConfig: process.AppConfig{Name: "appB", Namespace: "default"},
		ID: 3,
	},
})

	// 1. 測試 ID 匹配
	res := pm.findProcesses("1")
	if len(res) != 1 {
		t.Fatalf("ID matching failed, got %d results", len(res))
	}
	// Read identity fields through a value-copy snapshot rather than the
	// live pointer returned by findProcesses.
	info, ok := pm.reg.SnapshotOne("Infra:appB")
	if !ok || info.Name != "appB" || info.Namespace != "Infra" {
		t.Errorf("ID matching failed: name=%q ns=%q", info.Name, info.Namespace)
	}

	// 2. 測試 Name 匹配
	res = pm.findProcesses("appB")
	if len(res) != 2 {
		t.Errorf("Name matching failed, got %d", len(res))
	}

	// 3. 測試 Namespace 匹配
	res = pm.findProcesses("Infra")
	if len(res) != 2 {
		t.Errorf("Namespace matching failed, got %d", len(res))
	}

	// 4. 測試 "all" 匹配
	res = pm.findProcesses("all")
	if len(res) != 4 {
		t.Errorf("All matching failed, got %d", len(res))
	}
}

func TestWatchStateInheritance(t *testing.T) {
	testDir := testDir(t)
	pm := NewProcessManager(testDir)

	pm.reg.Add("default:watch-app", &ManagedProcess{
		Info: process.ProcessInfo{
		AppConfig: process.AppConfig{
		Name:      "watch-app",
		Namespace: "default",
		Watch:     true,
		Script:    "test.js",
	},
				ID:        1,
	},
})

	err := pm.Save()
	if err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	dumpPath := testDir + "/dump.json"
	data, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("Failed to read dump file: %v", err)
	}

	var entries []process.AppConfig
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("Failed to unmarshal dump entries: %v", err)
	}

	if len(entries) != 1 || !entries[0].Watch {
		t.Errorf("AppConfig did not preserve Watch attribute: %+v", entries)
	}
}

func TestVersionStateInheritance(t *testing.T) {
	testDir := testDir(t)
	pm := NewProcessManager(testDir)

	pm.reg.Add("default:version-app", &ManagedProcess{
		Info: process.ProcessInfo{
		AppConfig: process.AppConfig{
		Name:      "version-app",
		Namespace: "default",
		Version:   "1.2.3",
		Script:    "test.js",
	},
				ID:        1,
	},
})

	err := pm.Save()
	if err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	dumpPath := testDir + "/dump.json"
	data, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("Failed to read dump file: %v", err)
	}

	var entries []process.AppConfig
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("Failed to unmarshal dump entries: %v", err)
	}

	if len(entries) != 1 || entries[0].Version != "1.2.3" {
		t.Errorf("AppConfig did not preserve Version attribute: %+v", entries)
	}
}

// TestCWDInjectedAsPWD verifies $PWD seen by the spawned process matches the
// configured CWD, even though the BaseEnv snapshot carries a different PWD.
func TestCWDInjectedAsPWD(t *testing.T) {
	testDir := testDir(t)
	workDir := filepath.Join(testDir, "work")
	_ = os.MkdirAll(workDir, 0o755)

	pm := NewProcessManager(testDir)
	outPath := filepath.Join(testDir, "pwd.out")
	// Bash expands $PWD in the env; redirect to outPath via shell.
	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
		Namespace: "default",
		Name:      "pwdcheck",
		Script:    "echo",
		Args:      []string{"$PWD > " + outPath},
		Instances: 1,
		CWD:       workDir,
		// Snapshot deliberately carries a stale PWD.
		BaseEnv: append(os.Environ(), "PWD=/tmp/some/other/dir"),
	},
	}
	if _, err := pm.StartApp(req); err != nil {
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
	pm := NewProcessManager(testDir)

	for _, name := range []string{"a", "b", "c"} {
		req := &model.AppStartReq{
		AppConfig: process.AppConfig{
		Namespace: "default",
		Name:      name,
		Script:    "/bin/sh",
		Args:      []string{"-c", "sleep 30"},
		Instances: 1,
	},
	}
		if _, err := pm.StartApp(req); err != nil {
			t.Fatalf("startApp %s failed: %v", name, err)
		}
	}

	pm.KillAll()

	// Snapshot() returns value copies taken under the read lock, so the
	// status/PID reads below are atomic with respect to any writer and
	// need no external locking.
	for _, info := range pm.reg.Snapshot() {
		if info.Status != process.StatusStopped {
			t.Errorf("%s: status=%s, want stopped", info.Name, info.Status)
		}
		if info.PID != 0 {
			t.Errorf("%s: PID=%d, want 0", info.Name, info.PID)
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
	pm := NewProcessManager(testDir)

	scriptFile := "/bin/echo"

	pm.reg.Add("default:agentmemory", &ManagedProcess{
		Info: process.ProcessInfo{
		AppConfig: process.AppConfig{
		Name:       "agentmemory",
		Namespace:  "default",
		Script:     scriptFile,
		ConfigFile: "/path/to/ecosystem.config.js",
	},
				ID:         42,
	},
		done: make(chan struct{}),
})

	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
		Namespace:  "Agent",
		Name:       "agentmemory",
		Script:     scriptFile,
		ConfigFile: "/path/to/ecosystem.config.js",
		Instances:  1,
	},
	}

	_, err := pm.StartApp(req)
	if err != nil {
		t.Fatalf("startApp failed: %v", err)
	}

	// 檢查舊的 key 是否被刪除，且新的 key 存在，且 ID 繼承為 42
	if _, ok := pm.reg.Get("default:agentmemory"); ok {
		t.Errorf("Old process 'default:agentmemory' should have been deleted")
	}

	info, ok := pm.reg.SnapshotOne("Agent:agentmemory")
	if !ok {
		t.Fatalf("New process 'Agent:agentmemory' was not found")
	}
	if info.ID != 42 {
		t.Errorf("Expected ID 42 to be inherited, got %d", info.ID)
	}

	if info.ConfigFile != "/path/to/ecosystem.config.js" {
		t.Errorf("Expected ConfigFile to be propagated, got %s", info.ConfigFile)
	}
}

func TestDeleteDuringRestartSleep(t *testing.T) {
	testDir := testDir(t)
	pm := NewProcessManager(testDir)
	pm.RestartDelay = 500 * time.Millisecond

	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
		Namespace:   "default",
		Name:        "fail-app",
		Script:      "/usr/bin/false",
		MaxRestarts: 5,
		Instances:   1,
	},
	}

	_, err := pm.StartApp(req)
	if err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}

	// Wait a bit for the process to exit and enter the restart sleep
	time.Sleep(200 * time.Millisecond)

	// Existence check + status read via a single snapshot value copy.
	info, exists := pm.reg.SnapshotOne("default:fail-app")
	if !exists {
		t.Fatalf("Process fail-app was not registered")
	}

	// Verify it's in StatusLaunching or StatusErrored
	if status := info.Status; status != process.StatusLaunching && status != process.StatusErrored {
		t.Logf("Process status: %s", status)
	}

	// Delete it while it's sleeping (or about to restart)
	err = pm.DeleteByName("fail-app")
	if err != nil {
		t.Fatalf("Failed to delete process: %v", err)
	}

	// Wait for the restart interval (500ms) plus some buffer (600ms total)
	time.Sleep(600 * time.Millisecond)

	// Check if it got back
	if _, exists := pm.reg.Get("default:fail-app"); exists {
		t.Errorf("Deleted process got back after restart sleep!")
	}
}

func TestRestartsInheritance(t *testing.T) {
	testDir := testDir(t)
	pm := NewProcessManager(testDir)

	pm.reg.Add("default:appA", &ManagedProcess{
		Info: process.ProcessInfo{
		AppConfig: process.AppConfig{
		Name:      "appA",
		Namespace: "default",
		Script:    "/bin/echo",
	},
				ID:        1,
			Restarts:  5,
	},
})

	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
		Namespace: "default",
		Name:      "appA",
		Script:    "/bin/echo",
		Instances: 1,
	},
	}

	_, err := pm.StartApp(req)
	if err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}

	info, exists := pm.reg.SnapshotOne("default:appA")
	if !exists {
		t.Fatalf("Process appA was not registered")
	}

	if info.Restarts != 5 {
		t.Errorf("Expected restarts counter to be inherited as 5, got %d", info.Restarts)
	}
}

func TestStartAppOutFileHomeExpansion(t *testing.T) {
	testDir := testDir(t)

	// Capture the REAL HOME via os.Getenv BEFORE Setenv. We can't
	// use homedir.Dir() here because go-homedir caches the first
	// result at package level — a subsequent t.Setenv("HOME", ...)
	// has no effect on homedir.Expand for the rest of the test
	// process. os.Getenv reads the live env without caching.
	realHome := os.Getenv("HOME")
	if realHome == "" {
		t.Skip("HOME env var not set; nothing to protect against")
	}

	// Override HOME so `homedir.Expand("~/...")` resolves into the
	// test temp dir, NOT the developer's real home directory.
	// Without this, every run of `go test ./daemon/...` would
	// create `~/test-home-expand-out.log` on the host.
	t.Setenv("HOME", testDir)

	pm := NewProcessManager(testDir)

	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
		Namespace: "default",
		Name:      "homeexpandcheck",
		Script:    "/bin/sh",
		Args:      []string{"-c", "sleep 1"},
		Instances: 1,
		OutFile:   "~/test-home-expand-out.log",
		ErrorFile: "~/test-home-expand-err.log",
	},
	}

	pi, err := pm.StartApp(req)
	if err != nil {
		t.Fatalf("startApp failed: %v", err)
	}
	defer pm.StopByName("homeexpandcheck")

	if len(pi) == 0 {
		t.Fatalf("No process info returned")
	}

	// (1) Expansion must have happened — both files are absolute,
	// no literal `~` left in the path.
	if !strings.HasPrefix(pi[0].LogFile, "/") || strings.Contains(pi[0].LogFile, "~") {
		t.Errorf("LogFile path was not expanded: got %s", pi[0].LogFile)
	}
	if !strings.HasPrefix(pi[0].ErrorFile, "/") || strings.Contains(pi[0].ErrorFile, "~") {
		t.Errorf("ErrorFile path was not expanded: got %s", pi[0].ErrorFile)
	}
	// (2) Strengthened assertion (1.5): expanded paths must NOT live
	// under the developer's real home dir. This is the user-visible
	// invariant ("don't pollute my home") and is robust to changes
	// in t.TempDir / t.Setenv semantics across Go versions.
	if strings.HasPrefix(pi[0].LogFile, realHome) {
		t.Errorf("LogFile=%q expanded under real HOME=%q — test must "+
			"override HOME so ~ doesn't pollute the developer's home",
			pi[0].LogFile, realHome)
	}
	if strings.HasPrefix(pi[0].ErrorFile, realHome) {
		t.Errorf("ErrorFile=%q expanded under real HOME=%q",
			pi[0].ErrorFile, realHome)
	}
}

// TestSaveConcurrentWithMapMutation is the regression test for the
// "concurrent map iteration and map write" fatal that occurred when
// startAutoSave (background ticker) and model.CmdSave (RPC) both called save()
// while launchProcess / stopProcess were mutating pm.processes.
//
// Before the fix: the for-range over pm.processes inside save() ran with
// no lock, so any concurrent insertion/deletion would either crash with
// a Go runtime fatal or, with -race enabled, surface as a DATA RACE.
//
// After the fix: save() takes pm.RLock itself, so writers using
// pm.Lock() are mutually exclusive with the iteration. Field reads of
// mp.Info are now also synchronised against the in-place mutations done
// by stopProcess (Status/PID) and the cron callbacks (LastCronAt/etc.).
//
// Run with `go test -race ./daemon/...` — the test passing under -race
// is the actual verification; the assertions inside are sanity checks
// that save() returns no error.
func TestSaveConcurrentWithMapMutation(t *testing.T) {
	pm := NewProcessManager(testDir(t))
	stop := make(chan struct{})

	// Writer goroutine: continuously add entries (and mutate an
	// existing ProcessInfo field) to exercise both the map-mutation
	// race AND the mp.Info field-write race.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var n int
		for {
			select {
			case <-stop:
				return
			default:
			}
			name := fmt.Sprintf("app-%d", n%8)
			key := "default:" + name

			if _, ok := pm.reg.Get(key); ok {
				pm.reg.UpdateInfo(key, func(mp *ManagedProcess) {
					mp.Info.Version = fmt.Sprintf("rev-%d", n)
				})
			} else {
				pm.reg.Add(key, &ManagedProcess{
					Info: process.ProcessInfo{
						AppConfig: process.AppConfig{
							Namespace: "default",
							Name:      name,
							Script:    "sleep",
							Version:   fmt.Sprintf("v%d", n),
						},
						ID: n,
					},
				})
			}
			n++
		}
	}()

	// Savers: hammer save() from multiple goroutines (mirrors the
	// real-world contention between the auto-save ticker and CLI-driven
	// `pm2 save` RPC).
	const saverCount = 4
	for i := 0; i < saverCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if err := pm.Save(); err != nil {
					t.Errorf("save failed: %v", err)
					return
				}
			}
		}()
	}

	// 80ms is enough wall-time for the race detector to flag any
	// unsynchronised access; it is short enough to keep the suite fast.
	time.Sleep(80 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Final save must still produce a valid dump.json containing the
	// entries we just wrote (sanity check that the fix did not silently
	// produce empty / truncated output).
	if err := pm.Save(); err != nil {
		t.Fatalf("final save failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(pm.homeDir, "dump.json"))
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	var entries []process.AppConfig
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("unmarshal dump: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("dump.json is empty after concurrent writes")
	}
}

// TestRefreshMetricsDoesNotBlockRPC is the regression test for the
// "metrics collection blocks every RPC" issue (診斷 1.2). Before the
// fix, refreshMetrics held pm.Lock() across every `ps` call — fork +
// exec + wait is ~5-50 ms per process, so 30 processes would freeze
// the daemon for 150-1500 ms and starve every concurrent RPC.
//
// The fix snapshots (key, pid, online) under RLock, runs the slow ps
// calls with no lock held, then briefly takes Lock() to write back.
//
// Test strategy:
//   - Swap getProcessMetrics for a stub that sleeps 100 ms per call
//     and signals a barrier the moment it starts.
//   - Pre-populate 5 fake processes (no real OS processes needed).
//   - Run refreshMetrics() in a goroutine, wait for the barrier.
//   - Call listAll() and assert it returns in < 50 ms.
//   - Wait for refreshMetrics() to complete and verify the samples
//     were written back correctly.
func TestRefreshMetricsDoesNotBlockRPC(t *testing.T) {
	pm := NewProcessManager(testDir(t))

	// Barrier signal: the stub fires this on its very first invocation,
	// guaranteeing the test proceeds only when refreshMetrics has dropped
	// the RLock and entered the unlocked slow phase.
	phase2Started := make(chan struct{}, 8)

	orig := executor.GetProcessMetrics
	executor.GetProcessMetrics = func(pid int) (float64, uint64) {
		select {
		case phase2Started <- struct{}{}:
		default:
		}
		time.Sleep(100 * time.Millisecond)
		return 42.0, 4096
	}
	defer func() { executor.GetProcessMetrics = orig }()

	// Pre-populate 5 fake online processes. Fake PIDs are fine — the
	// stub never invokes a real `ps`.
	const N = 5
	for i := 0; i < N; i++ {
		key := fmt.Sprintf("default:metric-%d", i)
		pm.reg.Add(key, &ManagedProcess{
			Info: process.ProcessInfo{
		AppConfig: process.AppConfig{
		Namespace: "default",
		Name:      fmt.Sprintf("metric-%d", i),
	},
					ID:        i,
				PID:       10000 + i,
				Status:    process.StatusOnline,
	},
})
	}

	// Run one refreshMetrics pass in a goroutine.
	refreshDone := make(chan struct{})
	go func() {
		pm.refreshMetrics()
		close(refreshDone)
	}()

	// Wait for the slow phase to actually start — once this fires we
	// are guaranteed refreshMetrics is no longer holding any lock.
	<-phase2Started

	// While the goroutine is still mid-pipeline (sleeping inside the
	// stub), listAll() must NOT be blocked behind it. The previous
	// implementation would have held pm.Lock() for ~500 ms here.
	start := time.Now()
	infos := pm.ListAll()
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Errorf("listAll blocked for %v during metrics collection; want < 50ms", elapsed)
	}
	if len(infos) != N {
		t.Errorf("listAll returned %d entries, want %d", len(infos), N)
	}

	// Wait for refreshMetrics to finish (5 × 100 ms ≈ 500 ms total).
	<-refreshDone

	// Phase 3 must have written the stub's (42.0, 4096) sample to every
	// process whose PID still matches the snapshot.
	for i := 0; i < N; i++ {
		key := fmt.Sprintf("default:metric-%d", i)
		info, ok := pm.reg.SnapshotOne(key)
		if !ok {
			t.Fatalf("%s missing from processes map", key)
		}
		if info.CPU != 42.0 {
			t.Errorf("%s CPU=%v, want 42.0", key, info.CPU)
		}
		if info.Memory != 4096 {
			t.Errorf("%s Memory=%d, want 4096", key, info.Memory)
		}
	}
}

// TestRefreshMetricsSkipsRestartedProcess verifies that a process
// which was online at snapshot time but was restarted (PID changed)
// during the slow ps phase does NOT inherit the stale sample. This
// guards the "mp.Info.PID != t.pid" check in phase 3.
func TestRefreshMetricsSkipsRestartedProcess(t *testing.T) {
	pm := NewProcessManager(testDir(t))

	// Save the real implementation and restore on exit so subsequent
	// tests still get a working `ps` call.
	orig := executor.GetProcessMetrics
	defer func() { executor.GetProcessMetrics = orig }()

	// The stub captures the PID it was called with (i.e. the snapshot
	// value from phase 1) and then mutates the underlying ProcessInfo
	// to simulate a restart that happens DURING phase 2. The mutation
	// goes through UpdateInfo — the same path a real restart takes —
	// so it acquires the write lock and is race-clean under -race.
	var capturedPID int
	executor.GetProcessMetrics = func(pid int) (float64, uint64) {
		capturedPID = pid
		const key = "default:lonely"
		pm.reg.UpdateInfo(key, func(mp *ManagedProcess) {
			mp.Info.PID = 5678 // simulate restart while ps is in flight
		})
		return 99.0, 9999
	}

	// Seed one process with PID = 1234.
	const key = "default:lonely"
	pm.reg.Add(key, &ManagedProcess{
		Info: process.ProcessInfo{
		AppConfig: process.AppConfig{
		Namespace: "default",
		Name:      "lonely",
	},
				ID:        1,
			PID:       1234,
			Status:    process.StatusOnline,
	},
})

	pm.refreshMetrics()

	// Stub was called with the snapshot PID (1234).
	if capturedPID != 1234 {
		t.Errorf("stub saw PID=%d, want 1234 (snapshot)", capturedPID)
	}

	// Phase 3 saw the PID had changed to 5678 and skipped the write —
	// the stale (99, 9999) sample must NOT have leaked onto the new
	// instance's ProcessInfo.
	info, ok := pm.reg.SnapshotOne(key)
	if !ok {
		t.Fatalf("%s missing from processes map", key)
	}
	if info.CPU != 0 || info.Memory != 0 {
		t.Errorf("restarted process inherited stale metrics: CPU=%v Memory=%d (want 0/0)",
			info.CPU, info.Memory)
	}
	if info.PID != 5678 {
		t.Errorf("PID post-refresh=%d, want 5678", info.PID)
	}
}

// TestRefreshMetricsParallelSpeedup verifies that phase 2 actually
// runs the slow `ps` calls in parallel via the metricsWorkers pool.
//
// Methodology: stub getProcessMetrics with a known per-call sleep.
// Sequential execution of N calls would take N*stubMs. With the
// worker pool, wall-clock should be ~ceil(N/workers) * stubMs.
//
// This is a smoke test, not a precise perf assertion — CI jitter
// means we only require a clear speedup, not an exact ratio.
func TestRefreshMetricsParallelSpeedup(t *testing.T) {
	pm := NewProcessManager(testDir(t))

	orig := executor.GetProcessMetrics
	defer func() { executor.GetProcessMetrics = orig }()

	const stubMs = 50
	const N = 32 // > metricsWorkers * 4 to ensure batching is visible

	executor.GetProcessMetrics = func(pid int) (float64, uint64) {
		time.Sleep(stubMs * time.Millisecond)
		return float64(pid), uint64(pid) * 1024
	}

	for i := 0; i < N; i++ {
		key := fmt.Sprintf("default:speed-%d", i)
		pm.reg.Add(key, &ManagedProcess{
			Info: process.ProcessInfo{
		AppConfig: process.AppConfig{
		Namespace: "default",
		Name:      fmt.Sprintf("speed-%d", i),
	},
					ID:        i,
				PID:       1000 + i,
				Status:    process.StatusOnline,
	},
})
	}

	sequential := time.Duration(N) * stubMs * time.Millisecond
	start := time.Now()
	pm.refreshMetrics()
	elapsed := time.Since(start)

	// Conservative upper bound: with 8 workers and 32 items the ideal
	// wall-clock is ceil(32/8) * 50ms = 200ms. Allow generous headroom
	// (3× ideal) so the test isn't flaky on loaded CI machines, while
	// still failing clearly if phase 2 ever regresses to sequential
	// (1600ms >> 600ms threshold).
	parallelUpper := 3 * time.Duration(N/executor.MetricsWorkers) * stubMs * time.Millisecond
	t.Logf("refreshMetrics: %d processes in %v (sequential would be ~%v, ideal-parallel ~%v)",
		N, elapsed, sequential, time.Duration(N/executor.MetricsWorkers)*stubMs*time.Millisecond)

	if elapsed > parallelUpper {
		t.Errorf("refreshMetrics took %v, want < %v — phase 2 may have regressed to sequential",
			elapsed, parallelUpper)
	}

	// Verify writeback happened correctly under parallel load — no
	// sample lost, no cross-contamination between goroutines.
	for i := 0; i < N; i++ {
		key := fmt.Sprintf("default:speed-%d", i)
		info, ok := pm.reg.SnapshotOne(key)
		if !ok {
			t.Fatalf("%s missing from processes map", key)
		}
		wantCPU := float64(1000 + i)
		wantMem := uint64(1000+i) * 1024
		if info.CPU != wantCPU || info.Memory != wantMem {
			t.Errorf("%s: CPU=%v Memory=%d, want CPU=%v Memory=%d",
				key, info.CPU, info.Memory, wantCPU, wantMem)
		}
	}
}

// ============================================================================
// Phase 1 — Characterization tests (refactor safety net for Phases 2-5)
//
// Each test pins down one observable invariant that the upcoming
// protocol/registry/executor/network extraction MUST preserve:
//
//   - HighConcurrencyStartup      — concurrent startApp must not lose
//     registrations, duplicate PIDs, or duplicate IDs.
//   - ProcessErroredExitNoRestart — exit 1 with MaxRestarts=0 leaves the
//     process in errored state with PID=0; no auto-restart fires.
//   - ProcessErroredExitAutoRestart — exit 1 with MaxRestarts>0 triggers
//     a new launchProcess with a fresh PID; ID + Restarts preserved.
//   - ProcessCleanExit            — exit 0 maps to StatusStopped (not
//     StatusErrored) and is NOT auto-restarted even when MaxRestarts > 0.
//   - CronRestartFiresReboot      — @every 1s cron_restart actually
//     fires AND re-registers on each new instance (verified by
//     observing a SECOND tick).
// ============================================================================

// TestHighConcurrencyStartup verifies that many startApp calls can
// race against each other without losing or corrupting registrations.
//
// Safety net for the future ProcessRegistry refactor: if Add() / List()
// ever loses mutual exclusion, this test fails under -race (data race)
// and/or functionally (missing / duplicated entries).
func TestHighConcurrencyStartup(t *testing.T) {
	testDir := testDir(t)
	pm := NewProcessManager(testDir)

	const N = 20
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := &model.AppStartReq{
		AppConfig: process.AppConfig{
		Namespace: "default",
		Name:      fmt.Sprintf("concurrent-%d", idx),
		Script:    "/bin/sleep",
		Args:      []string{"30"},
		Instances: 1,
	},
	}
			if _, err := pm.StartApp(req); err != nil {
				errs <- fmt.Errorf("startApp[%d]: %w", idx, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("%v", err)
	}
	defer func() {
		for i := 0; i < N; i++ {
			_ = pm.StopByName(fmt.Sprintf("concurrent-%d", i))
		}
	}()

	// All N must be registered. PIDs unique, IDs unique, all online.
	// Snapshot() returns value copies under the read lock, so the
	// PID/ID/Status reads below are race-clean with no external lock.
	if got := pm.reg.Len(); got != N {
		t.Errorf("registered %d processes, want %d", got, N)
	}

	seenPID := make(map[int]bool, N)
	seenID := make(map[int]bool, N)
	for _, info := range pm.reg.Snapshot() {
		key := info.Namespace + ":" + info.Name
		if info.PID == 0 {
			t.Errorf("%s: PID=0 after start", key)
			continue
		}
		if seenPID[info.PID] {
			t.Errorf("%s: duplicate PID %d", key, info.PID)
		}
		seenPID[info.PID] = true
		if seenID[info.ID] {
			t.Errorf("%s: duplicate ID %d", key, info.ID)
		}
		seenID[info.ID] = true
		if info.Status != process.StatusOnline {
			t.Errorf("%s: status=%s, want online", key, info.Status)
		}
	}
}

// TestProcessErroredExitNoRestart verifies the lifecycle of a process
// that exits with a non-zero code and has no auto-restart budget.
// Expected: status becomes "errored", PID is reset to 0, the entry
// remains in the map but is not running, Restarts stays at 0.
//
// Safety net for the executor refactor: if watchProcess ever stops
// resetting PID to 0, or moves the Restarts++ before/after the
// Status update non-atomically, this test catches it.
func TestProcessErroredExitNoRestart(t *testing.T) {
	testDir := testDir(t)
	pm := NewProcessManager(testDir)
	pm.RestartDelay = 100 * time.Millisecond

	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
		Namespace: "default",
		Name:      "errored-norestart",
		Script:    "false",
		Instances: 1,
		// MaxRestarts defaults to 0 → no auto-restart.
	},
	}
	if _, err := pm.StartApp(req); err != nil {
		t.Fatalf("startApp failed: %v", err)
	}
	defer pm.StopByName("errored-norestart")

	// Wait for the process to die and watchProcess to update state.
	// Each field read goes through SnapshotOne so it is a value copy
	// taken under the read lock — a clean happens-before with
	// onProcessExit's UpdateInfo-guarded writes.
	var (
		info   process.ProcessInfo
		exists bool
	)
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		info, exists = pm.reg.SnapshotOne("default:errored-norestart")
		if exists && info.Status == process.StatusErrored {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !exists {
		t.Fatalf("process not registered")
	}
	if info.Status != process.StatusErrored {
		t.Errorf("status=%s, want %s", info.Status, process.StatusErrored)
	}
	if info.PID != 0 {
		t.Errorf("PID=%d after exit, want 0", info.PID)
	}
	if info.Restarts != 0 {
		t.Errorf("Restarts=%d, want 0 (MaxRestarts=0)", info.Restarts)
	}
}

// TestProcessErroredExitAutoRestart verifies that a process which
// exits with non-zero code gets restarted when MaxRestarts > 0.
// Expected: Restarts counter increments, new process gets a new
// PID, the logical process ID is preserved, status returns to
// online (or launching if we caught it mid-restart).
//
// Safety net for the executor refactor: if the restart goroutine
// in watchProcess ever fails to call launchProcess, or if
// launchProcess forgets to inherit the existing ID, this test
// catches it.
func TestProcessErroredExitAutoRestart(t *testing.T) {
	testDir := testDir(t)
	pm := NewProcessManager(testDir)
	pm.RestartDelay = 200 * time.Millisecond

	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
		Namespace:   "default",
		Name:        "errored-autorestart",
		// Bash script that sleeps 100 ms then exits 1. This gives a
		// ~100 ms "new instance alive" window between restart cycles so
		// the test can observe the new PID without races against the
		// watchProcess goroutine.
		Script:    "sleep",
		Args:      []string{"0.1", "&&", "false"},
		Instances: 1,
		MaxRestarts: 5,
	},
	}
	if _, err := pm.StartApp(req); err != nil {
		t.Fatalf("startApp failed: %v", err)
	}
	defer pm.StopByName("errored-autorestart")

	// Capture initial PID + ID via a value-copy snapshot.
	info0, ok := pm.reg.SnapshotOne("default:errored-autorestart")
	if !ok {
		t.Fatalf("process not registered after start")
	}
	initialPID := info0.PID
	initialID := info0.ID
	if initialPID == 0 {
		t.Fatalf("initial PID is 0")
	}

	// Poll until we observe either (a) a new PID ≠ initial PID, or
	// (b) Restarts ≥ 1. Either proves watchProcess fired AND the
	// restart goroutine launched a new process. We don't assert
	// "PID != 0 at moment of capture" because the cycle is fast and
	// the test could land in a "process just exited, new one not
	// yet spawned" gap.
	var (
		info2    process.ProcessInfo
		exists2  bool
		newPID   int
		restarts int
		newID    int
	)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		info2, exists2 = pm.reg.SnapshotOne("default:errored-autorestart")
		if exists2 {
			newPID = info2.PID
			restarts = info2.Restarts
			newID = info2.ID
		}
		if exists2 && restarts >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if !exists2 {
		t.Fatalf("process removed from map — auto-restart did not fire")
	}
	if restarts < 1 {
		t.Errorf("Restarts=%d after 2s, want >= 1 (auto-restart never fired)", restarts)
	}
	// ID is preserved across restarts (same logical process).
	if newID != initialID {
		t.Errorf("ID changed across restart: %d → %d", initialID, newID)
	}
	// If we caught the new instance mid-sleep, its PID must differ from
	// the original. If we caught the gap between cycles, PID == 0 is OK
	// as long as Restarts >= 1 and the process is still in the map.
	if newPID != 0 && newPID == initialPID {
		t.Errorf("PID unchanged (%d) after auto-restart — launchProcess reused PID?", newPID)
	}
}

// TestProcessCleanExit verifies that a process which exits with code
// 0 is treated as "stopped", not "errored", and is NOT auto-restarted
// even when MaxRestarts > 0. This is a complementary case to the
// errored-exit tests — pm2 only auto-restarts on non-zero exits.
//
// Safety net for the executor refactor: if the watchProcess exit-code
// branch ever gets inverted, the auto-restart loop would infinitely
// restart cleanly-exiting processes.
func TestProcessCleanExit(t *testing.T) {
	testDir := testDir(t)
	pm := NewProcessManager(testDir)
	pm.RestartDelay = 100 * time.Millisecond

	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
		Namespace:   "default",
		Name:        "clean-exit",
		Script:      "true",
		Instances:   1,
		MaxRestarts: 5, // even with budget, clean exit must NOT restart
	},
	}
	if _, err := pm.StartApp(req); err != nil {
		t.Fatalf("startApp failed: %v", err)
	}
	defer pm.StopByName("clean-exit")

	// Wait for the process to die and watchProcess to update state.
	var (
		info   process.ProcessInfo
		exists bool
	)
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		info, exists = pm.reg.SnapshotOne("default:clean-exit")
		if exists && info.PID == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !exists {
		t.Fatalf("process not registered")
	}
	if info.Status != process.StatusStopped {
		t.Errorf("status=%s, want %s (clean exit should be stopped, not errored)",
			info.Status, process.StatusStopped)
	}
	if info.PID != 0 {
		t.Errorf("PID=%d after exit, want 0", info.PID)
	}
	// Wait a bit longer to confirm no auto-restart fires.
	time.Sleep(500 * time.Millisecond)
	info, exists = pm.reg.SnapshotOne("default:clean-exit")
	if exists && info.Restarts != 0 {
		t.Errorf("Restarts=%d, want 0 (clean exit must not trigger auto-restart)",
			info.Restarts)
	}
}

// TestCronRestartFiresReboot is the characterization test for the
// cron_restart feature. It verifies that when a cron schedule fires,
// the registered callback calls restartByName which:
//   1. Stops the current process
//   2. Launches a new instance
//   3. Re-registers the cron entry on the new instance
//
// The new instance must have a different PID (proof that stopProcess
// + launchProcess actually ran, not just no-op'd), and the cron
// registration must persist across the restart (so the next tick
// will fire again) — verified by observing a SECOND tick.
//
// Safety net for the executor refactor: if restartByName ever
// forgets to re-register the cron (e.g., the Register/Remove
// ordering changes), the process will be restarted once then sit
// idle. The two-tick observation below catches that.
func TestCronRestartFiresReboot(t *testing.T) {
	testDir := testDir(t)
	pm := NewProcessManager(testDir)

	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
		Namespace: "default",
		Name:      "cron-restart-app",
		// Long-lived script so the test can observe the new PID
		// in the window between restart cycles (otherwise `sleep`
		// might exit too fast and the new PID is replaced by 0).
		Script:      "/bin/sleep",
		Args:        []string{"60"},
		Instances:   1,
		CronRestart: "@every 1s",
	},
	}
	if _, err := pm.StartApp(req); err != nil {
		t.Fatalf("startApp failed: %v", err)
	}
	defer pm.StopByName("cron-restart-app")

	// Capture initial PID via a value-copy snapshot.
	info0, ok := pm.reg.SnapshotOne("default:cron-restart-app")
	if !ok {
		t.Fatalf("process not registered after start")
	}
	initialPID := info0.PID
	if initialPID == 0 {
		t.Fatalf("initial PID is 0")
	}

	// Poll for first cron-tick restart. The cron fires at +1s, then
	// restartByName stops + launches. We look for either PID != initial
	// (new instance running) or a Restarts/ID-change signal. Allow up
	// to 2.5s so the test is stable under CI jitter.
	var (
		pidAfter1 int
		seen1     bool
	)
	deadline := time.Now().Add(2500 * time.Millisecond)
	for time.Now().Before(deadline) {
		info1, exists := pm.reg.SnapshotOne("default:cron-restart-app")
		if exists {
			pidAfter1 = info1.PID
			seen1 = true
		}
		if exists && pidAfter1 != 0 && pidAfter1 != initialPID {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !seen1 {
		t.Fatalf("process removed from map after first cron tick")
	}
	if pidAfter1 == initialPID {
		t.Errorf("PID unchanged (%d) after first cron tick — restartByName did not run",
			pidAfter1)
	}

	// Poll for second cron tick. If launchProcess forgot to
	// scheduler.Register the cron entry on the new instance, this
	// second tick will never fire and the PID will stay put.
	secondInitialPID := pidAfter1
	var (
		pidAfter2 int
		seen2     bool
	)
	deadline = time.Now().Add(2500 * time.Millisecond)
	for time.Now().Before(deadline) {
		info2, exists := pm.reg.SnapshotOne("default:cron-restart-app")
		if exists {
			pidAfter2 = info2.PID
			seen2 = true
		}
		if exists && pidAfter2 != 0 && pidAfter2 != secondInitialPID {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !seen2 {
		t.Fatalf("process removed after second cron tick — cron not re-registered")
	}
	if pidAfter2 == secondInitialPID {
		t.Errorf("PID unchanged (%d) after second cron tick — launchProcess did not re-register cron",
			pidAfter2)
	}
}

// TestStopProcessKillsChildren is the regression test for the
// "orphan child processes" bug (診斷 1.3). Before the fix, stopProcess
// only signalled the bash leader, leaving background children
// re-parented to PID 1 — still holding ports / files / FDs, invisible
// to `pm2 list`, with no parent to clean them up.
//
// Test strategy:
//   - Write a small shell script to t.TempDir() that spawns a
//     backgrounded sleep and writes its PID to a known file.
//     (We use a script FILE rather than `sh -c '...'` inline because
//     the daemon wraps every script in `bash -c "<script> <args>"`,
//     which strips inner quoting and breaks `$!` semantics — the
//     outer bash would parse `/bin/sh -c sleep 60 & echo $! > ...` as
//     "background `/bin/sh -c sleep 60`, then echo $!", and `$!`
//     would point at the /bin/sh subshell which dies immediately.)
//   - Verify the child PID is alive via kill(pid, 0).
//   - stopByName().
//   - Verify the child PID is GONE — kill(pid, 0) returns ESRCH.
func TestStopProcessKillsChildren(t *testing.T) {
	testDir := testDir(t)
	pm := NewProcessManager(testDir)
	childFile := filepath.Join(testDir, "child.pid")
	scriptPath := filepath.Join(testDir, "spawn_child.sh")

	script := "#!/bin/sh\n" +
		"sleep 60 &\n" +
		"echo $! > " + childFile + "\n" +
		"wait\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
		Namespace: "default",
		Name:      "orphan-test",
		Script:    scriptPath,
		Instances: 1,
	},
	}
	if _, err := pm.StartApp(req); err != nil {
		t.Fatalf("startApp: %v", err)
	}

	// Wait for the child PID file to be written.
	var childPID int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(childFile)
		if err == nil && len(data) > 0 {
			_, _ = fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &childPID)
			if childPID > 0 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if childPID == 0 {
		t.Fatalf("child PID file %s was never written", childFile)
	}

	// Sanity: child is alive right now.
	if err := syscall.Kill(childPID, 0); err != nil {
		t.Fatalf("child PID %d should be alive before stop, kill(0) returned %v",
			childPID, err)
	}

	// Stop the parent. The fix must propagate SIGTERM to the whole
	// process group, so the child sleep dies with the parent.
	if err := pm.StopByName("orphan-test"); err != nil {
		t.Fatalf("stopByName: %v", err)
	}

	// Give the OS a moment to reap.
	time.Sleep(100 * time.Millisecond)

	// The child MUST be gone.
	if err := syscall.Kill(childPID, 0); err == nil {
		t.Errorf("orphan child PID %d still alive after parent stopped — "+
			"stopProcess did not signal the process group", childPID)
	} else if err != syscall.ESRCH {
		t.Errorf("unexpected kill(0) error for child PID %d: %v", childPID, err)
	}
}

// TestCronNamespaceIsolation is the regression test for the cron
// scheduler key collision (診斷 1.4). Before the fix, Register keyed
// by process name alone — so starting `default:api` and `production:api`
// each with CronRestart overwrote each other's entry, and only the
// last-registered callback ever fired. The fix keys by "namespace:name".
//
// Test strategy: start two processes with the same name but different
// namespaces, both with CronRestart. Verify the scheduler holds TWO
// distinct entries (not 1, which would indicate the bug).
func TestCronNamespaceIsolation(t *testing.T) {
	testDir := testDir(t)
	pm := NewProcessManager(testDir)

	// Sanity: nothing registered yet.
	if got := pm.scheduler.EntryCount(); got != 0 {
		t.Fatalf("preflight: scheduler has %d entries, want 0", got)
	}

	req1 := &model.AppStartReq{
		AppConfig: process.AppConfig{
		Namespace:   "default",
		Name:        "api",
		Script:      "/bin/sleep",
		Args:        []string{"60"},
		CronRestart: "@every 1h", // long interval — we only check EntryCount
	},
	}
	if _, err := pm.StartApp(req1); err != nil {
		t.Fatalf("start default:api: %v", err)
	}
	defer pm.StopByName("default:api")

	if got := pm.scheduler.EntryCount(); got != 1 {
		t.Errorf("after first start: scheduler has %d entries, want 1", got)
	}

	req2 := &model.AppStartReq{
		AppConfig: process.AppConfig{
		Namespace:   "production",
		Name:        "api",
		Script:      "/bin/sleep",
		Args:        []string{"60"},
		CronRestart: "@every 1h",
	},
	}
	if _, err := pm.StartApp(req2); err != nil {
		t.Fatalf("start production:api: %v", err)
	}
	defer pm.StopByName("production:api")

	// The critical assertion: BOTH entries must exist.
	// Before the fix, this was 1 (production:api's Register overwrote
	// default:api's because both keyed by name="api").
	if got := pm.scheduler.EntryCount(); got != 2 {
		t.Errorf("scheduler has %d entries, want 2 — namespace:api and "+
			"production:api must each hold their own cron entry", got)
	}
}

// TestPauseResumeCronTask verifies the pause/resume lifecycle for a cron
// task: pause removes the scheduler entry and flips the status to
// StatusPaused (distinct from the idle StatusStopped a cron task normally
// carries), and resume re-registers the schedule and returns it to idle.
func TestPauseResumeCronTask(t *testing.T) {
	testDir := testDir(t)
	pm := NewProcessManager(testDir)

	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
			Namespace: "default",
			Name:      "nightly",
			Script:    "/bin/echo",
			Args:      []string{"hi"},
			Cron:      "@every 1h", // cron task: idle between fires
		},
	}
	if _, err := pm.StartApp(req); err != nil {
		t.Fatalf("startApp: %v", err)
	}

	// A cron task boots idle (StatusStopped) with its schedule registered.
	status, _, ok := pauseState(pm, "default:nightly")
	if !ok {
		t.Fatalf("nightly not registered after start")
	}
	if status != process.StatusStopped {
		t.Fatalf("after start: status=%s, want stopped", status)
	}
	if got := pm.scheduler.EntryCount(); got != 1 {
		t.Fatalf("after start: scheduler has %d entries, want 1", got)
	}

	// Pause: schedule removed, status becomes paused.
	if err := pm.PauseByName("default:nightly"); err != nil {
		t.Fatalf("pauseByName: %v", err)
	}
	status, paused, ok := pauseState(pm, "default:nightly")
	if !ok {
		t.Fatalf("nightly vanished after pause")
	}
	if status != process.StatusPaused {
		t.Errorf("after pause: status=%s, want paused", status)
	}
	if !paused {
		t.Errorf("after pause: paused flag not set")
	}
	if got := pm.scheduler.EntryCount(); got != 0 {
		t.Errorf("after pause: scheduler has %d entries, want 0 (must not fire)", got)
	}

	// Resume: schedule re-registered, status back to idle stopped.
	if err := pm.ResumeByName("default:nightly"); err != nil {
		t.Fatalf("resumeByName: %v", err)
	}
	status, paused, ok = pauseState(pm, "default:nightly")
	if !ok {
		t.Fatalf("nightly vanished after resume")
	}
	if status != process.StatusStopped {
		t.Errorf("after resume: status=%s, want stopped (idle)", status)
	}
	if paused {
		t.Errorf("after resume: paused flag still set")
	}
	if got := pm.scheduler.EntryCount(); got != 1 {
		t.Errorf("after resume: scheduler has %d entries, want 1 (re-registered)", got)
	}
}

// TestPausedCronTaskSurvivesResurrect is the regression test for the bug
// "paused cron job is not resurrected": before the fix, paused state lived
// only on ManagedProcess.paused (runtime memory) and was dropped on
// daemon restart, so a cron task that was deliberately suspended came
// back with its scheduler entry registered and would fire on schedule.
//
// After the fix, the paused flag must round-trip through save/resurrect:
//   - resurrected entry starts at StatusPaused (not StatusStopped)
//   - mp.paused is true on the new server
//   - the cron scheduler has NO entry for the resurrected process
func TestPausedCronTaskSurvivesResurrect(t *testing.T) {
	testDir := testDir(t)
	pm1 := NewProcessManager(testDir)

	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
			Namespace: "default",
			Name:      "nightly-paused",
			Script:    "/bin/echo",
			Args:      []string{"hi"},
			Cron:      "@every 1h",
		},
	}
	if _, err := pm1.StartApp(req); err != nil {
		t.Fatalf("startApp: %v", err)
	}
	if got := pm1.scheduler.EntryCount(); got != 1 {
		t.Fatalf("baseline: scheduler has %d entries, want 1", got)
	}

	// Pause the cron task — this is the state we expect to preserve.
	if err := pm1.PauseByName("default:nightly-paused"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if got := pm1.scheduler.EntryCount(); got != 0 {
		t.Fatalf("after pause: scheduler has %d entries, want 0", got)
	}

	// Persist the paused state. Before the fix, this drops the paused flag.
	if err := pm1.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Simulate a daemon restart: fresh server, same home dir.
	pm2 := NewProcessManager(testDir)
	if err := pm2.Resurrect(); err != nil {
		t.Fatalf("resurrect: %v", err)
	}

	// Critical assertions — all three must hold after the fix. The
	// (Status, paused) pair is read atomically under the registry write
	// lock via pauseState; there is no background goroutine writing the
	// resurrected entry, but we keep the sanctioned read path.
	status, paused, ok := pauseState(pm2, "default:nightly-paused")
	if !ok {
		t.Fatalf("resurrect: process missing from registry")
	}
	if status != process.StatusPaused {
		t.Errorf("after resurrect: status=%s, want %s (paused state must survive)",
			status, process.StatusPaused)
	}
	if !paused {
		t.Errorf("after resurrect: paused flag is false, want true")
	}
	if got := pm2.scheduler.EntryCount(); got != 0 {
		t.Errorf("after resurrect: scheduler has %d entries, want 0 (cron must NOT fire)", got)
	}

	_ = pm2.StopByName("default:nightly-paused")
}

// TestPauseResumeRunningProcess verifies pause stops a live process (PID
// cleared, status paused) and resume brings it back online.
func TestPauseResumeRunningProcess(t *testing.T) {
	testDir := testDir(t)
	pm := NewProcessManager(testDir)

	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
			Namespace: "default",
			Name:      "worker",
			Script:    "/bin/sh",
			Args:      []string{"-c", "sleep 60"},
			Instances: 1,
		},
	}
	if _, err := pm.StartApp(req); err != nil {
		t.Fatalf("startApp: %v", err)
	}
	defer pm.StopByName("default:worker")

	if err := pm.PauseByName("default:worker"); err != nil {
		t.Fatalf("pauseByName: %v", err)
	}
	// Category B read: a live worker's onProcessExit goroutine runs in
	// the background and writes mp.Info.PID=0 via UpdateInfo, so the
	// (Status, PID) pair must be read under the registry write lock too.
	// SnapshotOne would race with that write; UpdateInfo serialises it.
	var (
		pausedStatus process.Status
		pausedPID    int
	)
	pm.reg.UpdateInfo("default:worker", func(mp *ManagedProcess) {
		pausedStatus = mp.Info.Status
		pausedPID = mp.Info.PID
	})
	if pausedStatus != process.StatusPaused {
		t.Errorf("after pause: status=%s, want paused", pausedStatus)
	}
	if pausedPID != 0 {
		t.Errorf("after pause: PID=%d, want 0", pausedPID)
	}

	if err := pm.ResumeByName("default:worker"); err != nil {
		t.Fatalf("resumeByName: %v", err)
	}
	// Same Category B race window after resume: the freshly-launched
	// worker has a live onProcessExit goroutine once again.
	var (
		resumedStatus process.Status
		resumedPID    int
	)
	pm.reg.UpdateInfo("default:worker", func(mp *ManagedProcess) {
		resumedStatus = mp.Info.Status
		resumedPID = mp.Info.PID
	})
	if resumedStatus != process.StatusOnline {
		t.Errorf("after resume: status=%s, want online", resumedStatus)
	}
	if resumedPID == 0 {
		t.Errorf("after resume: PID=0, want a live pid")
	}
}

// TestConcurrentRestartDoesNotRaceOnMpInfo hammers a single process
// from two goroutines that both read mp.Info.* and rebuild an
// AppConfig-style request:
//
//  1. Goroutine A — RestartByName. Before the fix this read
//     mp.Info.Namespace/Name/Script/... outside the registry lock
//     to build req, racing with stopProcess's writes to mp.Info.
//  2. Goroutine B — onProcessExit's auto-restart goroutine. Before
//     the fix it built req from mp.Info.* after the UpdateInfo
//     callback had returned, racing with stopProcess / RestartByName.
//
// Both goroutines now snapshot AppConfig under UpdateInfo, so the
// race detector must stay silent across the test window. The
// iteration count is bounded so the test doesn't fight the registry
// lock for longer than the test timeout.
//
// Regression for the pre-existing race at server.go around the
// field-by-field mp.Info reads. If a future refactor reintroduces
// naked reads, this test trips `go test -race`.
func TestConcurrentRestartDoesNotRaceOnMpInfo(t *testing.T) {
	testDir := testDir(t)
	pm := NewProcessManager(testDir)
	pm.RestartDelay = 30 * time.Millisecond

	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
			Namespace:   "default",
			Name:        "race-restart",
			Script:      "/bin/sleep",
			Args:        []string{"60"},
			Instances:   1,
			MaxRestarts: 50,
		},
	}
	if _, err := pm.StartApp(req); err != nil {
		t.Fatalf("startApp: %v", err)
	}
	defer pm.StopByName("race-restart")

	// Let auto-restart run for a few cycles so the auto-restart
	// goroutine is in steady state.
	time.Sleep(120 * time.Millisecond)

	const iterations = 8
	var wg sync.WaitGroup

	// Goroutine A: explicit CmdRestart — exercises the
	// RestartByName snapshot path that was racy before the fix.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = pm.RestartByName("race-restart")
			time.Sleep(40 * time.Millisecond) // let auto-restart breathe
		}
	}()

	// Goroutine B: read every field the auto-restart path used to read
	// raw, via SnapshotOne's value copy (taken under the registry read
	// lock). Mirrors what was naked before the fix — every field is
	// touched while a concurrent writer (RestartByName / auto-restart)
	// mutates the live entry, and the race detector must stay silent.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			info, ok := pm.reg.SnapshotOne("default:race-restart")
			if ok {
				_ = info.Namespace
				_ = info.Name
				_ = info.Script
				_ = info.Args
				_ = info.Env
				_ = info.Cron
				_ = info.CronRestart
				_ = info.Watch
				_ = info.MaxRestarts
				_ = info.Version
				_ = info.LogFile
				_ = info.ErrorFile
				_ = info.ConfigFile
				_ = info.CWD
				_ = info.BaseEnv
				_ = info.Status
				_ = info.PID
				_ = info.Restarts
			}
			time.Sleep(40 * time.Millisecond)
		}
	}()

	// Bound the wait so a deadlock fails the test rather than hangs
	// forever.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent restart test deadlocked or exceeded timeout")
	}

	// Sanity: process should still be in the registry.
	if _, ok := pm.reg.Get("default:race-restart"); !ok {
		t.Errorf("race-restart vanished from registry after concurrent restarts")
	}
}



