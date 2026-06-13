package daemon

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/shuk/pm2/process"
)

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


