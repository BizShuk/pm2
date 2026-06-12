package daemon

import (
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
