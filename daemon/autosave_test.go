package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
)

// dumpNames reads dump.json and returns the "namespace:name" of every
// persisted entry, so tests can assert what the daemon believes it should
// resurrect without depending on the rest of AppConfig.
func dumpNames(t *testing.T, homeDir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(homeDir, "dump.json"))
	if err != nil {
		t.Fatalf("read dump.json: %v", err)
	}
	var entries []process.AppConfig
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("decode dump.json: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Namespace+":"+e.Name)
	}
	return names
}

func startSleeper(t *testing.T, pm *ProcessManager, name string) {
	t.Helper()
	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
			Namespace: "default",
			Name:      name,
			Script:    "sleep",
			Args:      []string{"30"},
			Instances: 1,
		},
	}
	if _, err := pm.StartApp(req); err != nil {
		t.Fatalf("startApp %s: %v", name, err)
	}
}

// A registered app must reach dump.json without an explicit `pm2 save`,
// otherwise a daemon restart within the auto-save interval loses it.
func TestStartAppAutoSaves(t *testing.T) {
	home := testDir(t)
	pm := NewProcessManager(home)
	t.Cleanup(func() { _ = pm.StopByName("all") })

	startSleeper(t, pm, "autosave-a")

	names := dumpNames(t, home)
	if len(names) != 1 || names[0] != "default:autosave-a" {
		t.Fatalf("dump.json = %v, want [default:autosave-a]", names)
	}
}

// Deleting must rewrite the dump too — the case that motivated this hook:
// `pm2 apply --delete` followed by a daemon restart used to resurrect the
// tasks that were just deleted.
func TestDeleteByNameAutoSaves(t *testing.T) {
	home := testDir(t)
	pm := NewProcessManager(home)
	t.Cleanup(func() { _ = pm.StopByName("all") })

	startSleeper(t, pm, "autosave-a")
	startSleeper(t, pm, "autosave-b")

	if err := pm.DeleteByName("default:autosave-a"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	names := dumpNames(t, home)
	if len(names) != 1 || names[0] != "default:autosave-b" {
		t.Fatalf("dump.json = %v, want [default:autosave-b]", names)
	}
}

// A delete that matches nothing changes no membership, so it must leave
// the dump exactly as it was rather than rewriting it.
func TestFailedDeleteLeavesDumpUntouched(t *testing.T) {
	home := testDir(t)
	pm := NewProcessManager(home)
	t.Cleanup(func() { _ = pm.StopByName("all") })

	startSleeper(t, pm, "autosave-a")
	before, err := os.ReadFile(filepath.Join(home, "dump.json"))
	if err != nil {
		t.Fatalf("read dump.json: %v", err)
	}

	if err := pm.DeleteByName("no-such-task"); err == nil {
		t.Fatal("expected delete of unknown task to fail")
	}

	after, err := os.ReadFile(filepath.Join(home, "dump.json"))
	if err != nil {
		t.Fatalf("read dump.json: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("dump.json rewritten by a failed delete:\nbefore=%s\nafter=%s", before, after)
	}
}

// Resurrect replays the dump; a per-entry launch failure must not write
// the surviving subset back over the file, which would erase the failed
// app's saved config for good.
func TestResurrectDoesNotRewriteDump(t *testing.T) {
	home := testDir(t)
	entries := []process.AppConfig{
		{Namespace: "default", Name: "autosave-a", Script: "sleep", Args: []string{"30"}, Instances: 1},
		{Namespace: "default", Name: "autosave-broken", Script: "/nonexistent/binary", Instances: 1},
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatalf("encode dump: %v", err)
	}
	dumpPath := filepath.Join(home, "dump.json")
	if err := os.WriteFile(dumpPath, data, 0o644); err != nil {
		t.Fatalf("write dump.json: %v", err)
	}

	pm := NewProcessManager(home)
	t.Cleanup(func() { _ = pm.StopByName("all") })
	if err := pm.Resurrect(); err != nil {
		t.Fatalf("resurrect: %v", err)
	}

	after, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("read dump.json: %v", err)
	}
	if string(after) != string(data) {
		t.Fatalf("resurrect rewrote dump.json:\nbefore=%s\nafter=%s", data, after)
	}
	if pm.suppressAutoSave.Load() {
		t.Error("suppressAutoSave still set after Resurrect returned")
	}
}
