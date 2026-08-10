# 進程命名空間實作計畫 (Process Namespace Implementation Plan)

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

`目標`：在 `pm2` 中為每個進程引入 `命名空間 (namespace)` 的概念，預設為 `default`，並在進程操作中實作 `ID` > `Name` > `Namespace` 的匹配優先順序。

`架構`：擴展 `ProcessInfo` 與設定檔結構。在 `daemon` 端的 `Server.processes` 使用 `Namespace:Name` 作為 unique key。提供 `findProcesses` 函數來進行多層級篩選，並更新相關 CLI 指令與 TUI。

`技術棧`：Go 1.24+, `spf13/cobra`, `charmbracelet/bubbletea`.

---

### Task 1: 擴展資料結構與設定檔 (Extend Data Structures and Config)

`檔案`：

- 修改：[process/types.go](file:///Users/shuk/projects/pm2/process/types.go)
- 修改：[config/ecosystem.go](file:///Users/shuk/projects/pm2/config/ecosystem.go)
- 修改：[daemon/protocol.go](file:///Users/shuk/projects/pm2/daemon/protocol.go)

- [ ] `Step 1`：修改 `process/types.go`，在 `ProcessInfo` 和 `DumpEntry` 結構體中新增 `Namespace` 欄位。

```go
// process/types.go
// 於 ProcessInfo 中新增：
Namespace string `json:"namespace"`
// 於 DumpEntry 中新增：
Namespace string `json:"namespace"`
```

- [ ] `Step 2`：修改 `config/ecosystem.go`，在 `AppConfig` 中新增 `Namespace` 欄位，並修改 `Normalize()` 將空命名空間預設為 `default`。

```go
// config/ecosystem.go
// 於 AppConfig 中新增：
Namespace string `json:"namespace"`

// 修改 Normalize() 方法：
if a.Namespace == "" {
    a.Namespace = "default"
}
```

- [ ] `Step 3`：修改 `daemon/protocol.go`，在 `AppStartReq` 中新增 `Namespace` 欄位。

```go
// daemon/protocol.go
// 於 AppStartReq 中新增：
Namespace string `json:"namespace"`
```

- [ ] `Step 4`：編譯專案確認資料結構無語法錯誤。

執行：`go build -o /dev/null ./...`
預期：編譯通過

---

### Task 2: 實作與測試 Daemon 匹配邏輯 (Implement and Test Daemon Match Logic)

`檔案`：

- 修改：[daemon/server.go](file:///Users/shuk/projects/pm2/daemon/server.go)
- 新增：[daemon/server_test.go](file:///Users/shuk/projects/pm2/daemon/server_test.go)

- [ ] `Step 1`：在 `daemon` 目錄下建立單元測試 `server_test.go`。先寫一個會失敗的測試，用以測試 `findProcesses`。

```go
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

 // 2. 測試 Name 匹配（應找出所有同名進程）
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
```

- [ ] `Step 2`：執行測試，確認其編譯失敗（因為 `findProcesses` 還沒定義）。

執行：`go test -v ./daemon`
預期：編譯失敗，提示 `s.findProcesses undefined`

- [ ] `Step 3`：在 `daemon/server.go` 中實作 `findProcesses` 邏輯。

```go
func (s *Server) findProcesses(target string) []*ManagedProcess {
 s.mu.RLock()
 defer s.mu.RUnlock()

 if target == "all" {
  var list []*ManagedProcess
  for _, mp := range s.processes {
   list = append(list, mp)
  }
  return list
 }

 // 1. ID 匹配
 var idVal int
 if _, err := fmt.Sscan(target, &idVal); err == nil {
  for _, mp := range s.processes {
   if mp.Info.ID == idVal {
    return []*ManagedProcess{mp}
   }
  }
 }

 // 2. Name 匹配
 var matchedByName []*ManagedProcess
 for _, mp := range s.processes {
  if mp.Info.Name == target {
   matchedByName = append(matchedByName, mp)
  }
 }
 if len(matchedByName) > 0 {
  return matchedByName
 }

 // 3. Namespace 匹配
 var matchedByNS []*ManagedProcess
 for _, mp := range s.processes {
  if mp.Info.Namespace == target {
   matchedByNS = append(matchedByNS, mp)
  }
 }
 return matchedByNS
}
```

- [ ] `Step 4`：執行 `go test -v ./daemon` 確認單元測試通過。

執行：`go test -v ./daemon`
預期：測試通過 (PASS)

---

### Task 3: 修改 Daemon 內部狀態管理與 key 值 (Update Daemon State and Storage Key)

`檔案`：

- 修改：[daemon/server.go](file:///Users/shuk/projects/pm2/daemon/server.go)

- [ ] `Step 1`：修改 `daemon/server.go` 中的 `startApp` 函數，使用 `req.Namespace + ":" + name` 作為 `processes` map 的鍵值 (key) 進行重複檢查，並使用複合鍵停止舊進程。

```go
// 修改前：
// if existing, ok := s.processes[name]; ok { ... }
// 修改後：
s.mu.Lock()
key := req.Namespace + ":" + name
if existing, ok := s.processes[key]; ok {
    if existing.Info.Script != req.Script {
        s.mu.Unlock()
        return infos, fmt.Errorf(
            "process %q already exists with script %q; use 'pm2 delete %s' first or use a different name",
            name, existing.Info.Script, name,
        )
    }
    s.mu.Unlock()
    _ = s.stopProcess(existing)
} else {
    s.mu.Unlock()
}
```

- [ ] `Step 2`：修改 `daemon/server.go` 中的 `launchProcess` 函數。在儲存新進程至 `s.processes` 時使用複合鍵 `mp.Info.Namespace + ":" + name`，並把 `Namespace` 填入 `process.ProcessInfo`。

```go
// 修改前：
// s.processes[name] = mp
// 修改後：
mp.Info.Namespace = req.Namespace
s.processes[req.Namespace+":"+name] = mp
```

- [ ] `Step 3`：修改 `daemon/server.go` 中的 `watchProcess` 自動重啟邏輯，重啟時傳入正確的 `Namespace` 欄位且啟動複合鍵。

```go
// 修改前：
// _, _ = s.launchProcess(mp.Info.Name, req)
// 修改後：
req := &AppStartReq{
    Namespace:   mp.Info.Namespace,
    Name:        mp.Info.Name,
    Script:      mp.Info.Script,
    Args:        mp.Info.Args,
    Env:         mp.Info.Env,
    CronRestart: mp.Info.CronRestart,
    MaxRestarts: mp.Info.MaxRestarts,
    LogFile:     mp.Info.LogFile,
    ErrorFile:   mp.Info.ErrorFile,
    Instances:   1,
}
_, _ = s.launchProcess(mp.Info.Name, req)
```

- [ ] `Step 4`：修改 `daemon/server.go` 中的 `stopByName`、`restartByName`、`deleteByName` 函數，使用 `findProcesses` 來尋找目標。

```go
// 重構 stopByName：
func (s *Server) stopByName(name string) error {
 targets := s.findProcesses(name)
 if len(targets) == 0 {
  return fmt.Errorf("process or namespace not found: %s", name)
 }
 for _, mp := range targets {
  _ = s.stopProcess(mp)
 }
 return nil
}

// 重構 restartByName：
func (s *Server) restartByName(name string) error {
 targets := s.findProcesses(name)
 if len(targets) == 0 {
  return fmt.Errorf("process or namespace not found: %s", name)
 }
 for _, mp := range targets {
  req := &AppStartReq{
   Namespace:   mp.Info.Namespace,
   Name:        mp.Info.Name,
   Script:      mp.Info.Script,
   Args:        mp.Info.Args,
   Env:         mp.Info.Env,
   CronRestart: mp.Info.CronRestart,
   MaxRestarts: mp.Info.MaxRestarts,
   LogFile:     mp.Info.LogFile,
   ErrorFile:   mp.Info.ErrorFile,
   Instances:   1,
  }
  _ = s.stopProcess(mp)
  _, _ = s.launchProcess(mp.Info.Name, req)
 }
 return nil
}

// 重構 deleteByName：
func (s *Server) deleteByName(name string) error {
 targets := s.findProcesses(name)
 if len(targets) == 0 {
  return fmt.Errorf("process or namespace not found: %s", name)
 }
 for _, mp := range targets {
  _ = s.stopProcess(mp)
  s.mu.Lock()
  delete(s.processes, mp.Info.Namespace+":"+mp.Info.Name)
  s.mu.Unlock()
 }
 return nil
}
```

- [ ] `Step 5`：修改 `save` 與 `resurrect` 函數，儲存與復原 `Namespace`。

```go
// 於 save() 寫入 DumpEntry 時填入 Namespace：
Namespace: mp.Info.Namespace,

// 於 resurrect() 建立 AppStartReq 時填入 Namespace：
Namespace: e.Namespace,
```

- [ ] `Step 6`：執行 `go test -v ./daemon` 確認測試通過。

---

### Task 4: 更新 CLI 指令與日誌篩選 (Update CLI Commands and Logs Filtering)

`檔案`：

- 修改：[cmd/start.go](file:///Users/shuk/projects/pm2/cmd/start.go)
- 修改：[cmd/list.go](file:///Users/shuk/projects/pm2/cmd/list.go)
- 修改：[cmd/logs.go](file:///Users/shuk/projects/pm2/cmd/logs.go)

- [ ] `Step 1`：修改 `cmd/start.go`，在 `newStartCmd()` 增加 `--namespace` (簡寫 `-ns`) flag，並填入 `AppStartReq` 中。

```go
// 在 newStartCmd() 的變數宣告中新增：
var namespace string

// 在 cmd.RunE 中，若是 Bare script path 則套用 namespace：
app := config.SingleApp(target, name, scriptArgs)
if namespace != "" {
    app.Namespace = namespace
}

// 在建構 daemon.Request 時，傳入 namespace：
req := daemon.Request{
    Command: daemon.CmdStart,
    App: &daemon.AppStartReq{
        Namespace:   app.Namespace,
        Name:        app.Name,
        // ... 其他欄位保持不變
    },
}

// 在 cmd 下方新增 flag 綁定：
cmd.Flags().StringVar(&namespace, "namespace", "", "process namespace")
cmd.Flags().StringVar(&namespace, "ns", "", "process namespace (shortcut)")
```

- [ ] `Step 2`：修改 `cmd/list.go`，在表格輸出中加入 `Namespace` 欄位。

```go
// 修改 table.SetHeader：
table.SetHeader([]string{"ID", "Namespace", "Name", "PID", "Status", "Restarts", "Cron"})

// 修改 table.Append 列印內容：
table.Append([]string{
    fmt.Sprintf("%d", p.ID),
    p.Namespace,
    p.Name,
    pid,
    string(p.Status),
    fmt.Sprintf("%d", p.Restarts),
    p.CronRestart,
})
```

- [ ] `Step 3`：修改 `cmd/logs.go`，依據 `ID` > `Name` > `Namespace` 順序進行日誌篩選。

```go
// 修改 cmd/logs.go 中對 logFiles 的收集邏輯：
// 先找是否匹配 ID，若有則只拿該 ID 的進程。
// 若無，再找是否匹配 Name，若有則收集所有同名進程。
// 若無，再找是否匹配 Namespace，若有則收集該 namespace 下所有進程。

var matchedProcs []process.ProcessInfo
if filterName == "" {
    matchedProcs = infos
} else {
    // 1. 嘗試 ID 匹配
    var idVal int
    isID := false
    if _, err := fmt.Sscan(filterName, &idVal); err == nil {
        isID = true
    }
    if isID {
        for _, p := range infos {
            if p.ID == idVal {
                matchedProcs = append(matchedProcs, p)
            }
        }
    }
    // 2. 若非 ID，嘗試 Name 匹配
    if len(matchedProcs) == 0 {
        for _, p := range infos {
            if p.Name == filterName {
                matchedProcs = append(matchedProcs, p)
            }
        }
    }
    // 3. 若非 Name，嘗試 Namespace 匹配
    if len(matchedProcs) == 0 {
        for _, p := range infos {
            if p.Namespace == filterName {
                matchedProcs = append(matchedProcs, p)
            }
        }
    }
}

// 根據 matchedProcs 收集日誌：
for _, p := range matchedProcs {
    if p.LogFile != "" {
        logFiles = append(logFiles, p.LogFile)
    }
    if p.ErrorFile != "" {
        logFiles = append(logFiles, p.ErrorFile)
    }
}
```

---

### Task 5: 修改 TUI 監控面板 (Update TUI Monitor Panel)

`檔案`：

- 修改：[tui/model.go](file:///Users/shuk/projects/pm2/tui/model.go)

- [ ] `Step 1`：修改 `tui/model.go` 中的 `detailRows` 常數改為 `11`。

```go
// 修改前：
// detailRows = 10
// 修改後：
detailRows = 11
```

- [ ] `Step 2`：在 `buildDetail` 函數 of rows 中新增 `namespace` 列。

```go
// 在 rows 中新增一項：
{"namespace", p.Namespace, ""},
```

- [ ] `Step 3`：修改 `tui/model.go` 鍵盤處理邏輯，操作時將 `daemon.Request.Name` 改為傳送進程 ID。

```go
// 修改前：
// name := m.procs[m.selected].Name
// case "r": return m, doAction(m.socket, daemon.Request{Command: daemon.CmdRestart, Name: name})
// 修改後：
targetID := fmt.Sprintf("%d", m.procs[m.selected].ID)
// case "r": return m, doAction(m.socket, daemon.Request{Command: daemon.CmdRestart, Name: targetID})
// case "s": return m, doAction(m.socket, daemon.Request{Command: daemon.CmdStop, Name: targetID})
// case "d": return m, doAction(m.socket, daemon.Request{Command: daemon.CmdDelete, Name: targetID})
```

- [ ] `Step 4`：編譯整個專案，確認編譯無誤。

執行：`go build -o pm2 .`
預期：成功編譯出 `pm2` 執行檔。
