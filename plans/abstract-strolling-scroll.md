# 第三階段：抽離進程註冊表 (Extract ProcessRegistry)

## Context

`daemon.Server` 目前以兩個直接欄位持有進程狀態 — `mu sync.RWMutex`（`daemon/server.go:29`）與 `processes map[string]*ManagedProcess`（`daemon/server.go:30`）— 並在 `daemon/server.go`、`daemon/manager.go`、`daemon/metrics.go`、`daemon/persistence.go`、`daemon/server_test.go` 共 **155 處**以裸 `s.mu.Lock/RUnlock` 與 `s.processes[key]` 形式操作。這違反了 `daemon/process_registry.go` 抽取的單一職責原則：狀態儲存、鎖管理、與業務調度三件事散落在同一個套件的多個檔案中。

來源計畫：
- [plans/architecture-extract-registry.md](../plans/architecture-extract-registry.md)（**主計畫**：4 階段，已完整列出 `s.processes` / `s.mu` 在 8 個檔案的所有 26 處引用）
- [plans/architecture-daemon-decoupling-phase3.md](../plans/architecture-daemon-decoupling-phase3.md)（**互補版本**：4 階段，第一階段即本任務）
- [plans/stateful-plotting-mountain.md](../plans/stateful-plotting-mountain.md)（**5-Phase Strangler-Fig Plan**：Phase 3 設計了 `Registry` + escape hatch pattern）

兩個計畫在 `extract-registry.md §7 風險與回滾策略` 與 `stateful-plotting-mountain.md Phase 3 變更清單` 都明確指引了測試相容策略：

> `對策`：保持相容性，可以在 `Server` 提供一個輔助測試的導出方法 `ProcessesForTest() map[string]*ManagedProcess`（或直接在測試中調用 `s.registry`），確保測試代碼仍能正常工作。

> `保留 `Server.mu` 介面`：daemon/server_test.go 有 30+ 處直接 `s.mu.RLock()` 讀 `s.processes[key]`，改 method 化要全改測試。

**目標**：以 `daemon/process_registry.go::ProcessRegistry` 為唯一鎖持有者與 map 擁有者；`Server` 改為委派。`mu` 與 `processes` 直接欄位從 `Server` 移除。`daemon/server_test.go` 機械式更新為 `s.RLock/RUnlock/Lock/Unlock`（Server 委派方法）與 `s.reg.Add/Get/Remove`。

---

## 設計重點

### 1. ProcessRegistry 結構

```go
// daemon/process_registry.go
package manager

import (
    "sync"
    "github.com/bizshuk/pm2/daemon"            // 取得 ManagedProcess 型別
    "github.com/bizshuk/pm2/process"
)

// ProcessRegistry 封裝進程 map 與讀寫鎖；對外提供線程安全 CRUD 與
// 原子化狀態更新方法。是 daemon.Server 唯一持鎖者。
type ProcessRegistry struct {
    mu        sync.RWMutex
    processes map[string]*daemon.ManagedProcess
}

func NewProcessRegistry() *ProcessRegistry {
    return &ProcessRegistry{processes: make(map[string]*daemon.ManagedProcess)}
}
```

> **關鍵設計**：`ProcessRegistry` 與 `daemon.ManagedProcess` 之間有循環依賴風險 — `ManagedProcess` 定義在 `daemon` 包，`ProcessRegistry` 也要在 `daemon` 包內使用。
> **解法**：將 `ProcessRegistry` 放在 `daemon/process_registry.go`，屬於 `daemon.ManagerState` 子包（`daemon/manager/`），import daemon 主包。**單向依賴**：`manager` → `daemon`（OK），`daemon` → `manager`（也 OK，因為 `daemon/manager` 是子包）。
> 但 `daemon/server.go` 需要同時持有 `ProcessRegistry` 與 `ManagedProcess` — 都來自 `daemon` 主包與 `daemon/manager` 子包，無循環問題。

### 2. ProcessRegistry API

| 方法 | 用途 | 內部鎖 |
|---|---|---|
| `Add(key string, mp *ManagedProcess)` | 註冊；冪等取代既有 entry | `Lock` |
| `Get(key string) (*ManagedProcess, bool)` | 取得單一 mp | `RLock` |
| `Remove(key string) (mp *ManagedProcess, removed bool)` | 移除並回傳 | `Lock` |
| `List() []*ManagedProcess` | 全部 mp 的 slice 快照 | `RLock` |
| `Snapshot() []process.ProcessInfo` | 給 `save()` 用的 `AppConfig` 快照 | `RLock` |
| `Len() int` | map 大小 | `RLock` |
| `FindByTarget(target string) []*ManagedProcess` | ID/exact/name/namespace/"all" 多策略搜尋（取代 `findProcesses`） | `RLock` |
| `UpdateInfo(key string, fn func(*ManagedProcess))` | 原子化 mutate；給 `watchProcess` 改 PID/Status、`pauseByName` 改 `paused` 等 | `Lock` |
| `UpdateMetrics(key string, cpu float64, mem uint64)` | 原子化 CPU/Mem 寫入；含 PID check 防 stale 樣本 | `Lock` |
| `UpdateCronStatus(key string, firedAt time.Time, status string)` | cron 觸發回寫 `LastCronAt` / `LastCronStatus` | `Lock` |
| `Lock/Unlock/RLock/RUnlock()` | escape hatch — 給 `deleteByName` 等需要跨多次呼叫持鎖的場景 | 委派內部 mu |

`UpdateInfo(key, fn)` 的設計特別重要 — 取代目前 7 處「裸取 mp + Lock + 改欄位 + Unlock」三步模式（如 `daemon/server.go:541-562` `stopProcess` 結尾、`daemon/server.go:557-562` watchProcess 重啟檢查）。這是 [plans/architecture-extract-registry.md §1.3](../plans/architecture-extract-registry.md) 點名的反模式。

### 3. Server 結構改動

```go
// daemon/server.go
type Server struct {
    reg         *manager.ProcessRegistry   // 取代 mu + processes
    nextID      int
    homeDir     string
    scheduler   *cron.Scheduler
    RestartDelay time.Duration
}

// Server-level lock delegate（escape hatch）。測試原本寫 `s.mu.RLock()`
// 改為 `s.RLock()`，行為完全相同（底層委派給 s.reg.mu）。
func (s *Server) Lock()    { s.reg.Lock() }
func (s *Server) Unlock()  { s.reg.Unlock() }
func (s *Server) RLock()   { s.reg.RLock() }
func (s *Server) RUnlock() { s.reg.RUnlock() }

// 測試輔助：完整 map 快照（深拷 mp 指標，不是 deep clone — 測試只讀欄位）
func (s *Server) ProcessesForTest() map[string]*ManagedProcess {
    out := s.reg.List()  // []*ManagedProcess
    m := make(map[string]*ManagedProcess, len(out))
    for _, mp := range out {
        m[mp.Info.Namespace+":"+mp.Info.Name] = mp
    }
    return m
}
```

> 為什麼需要 `ProcessesForTest()`：現有測試（如 `TestFindProcesses`、`TestWatchStateInheritance`）會**直接寫** `s.processes["key"] = &ManagedProcess{...}` 來預設測試資料。`List()` 回傳 slice，不能直接賦值；提供 `ProcessesForTest()` map 視圖讓這類測試可以機械式更新為 `s.ProcessesForTest()["key"] = ...`。但測試仍要拿 `s.reg.Lock()` 才能寫。
> **改採用更乾淨的做法**：測試改用 `s.reg.Add(key, mp)`（已是 thread-safe），這樣不需要 `ProcessesForTest()` 的 map 寫入 API；只在測試需要迭代時用 `s.reg.List()`。

最終決定 — **不提供 `ProcessesForTest()` map 寫入**：所有測試的 `s.processes[k] = &ManagedProcess{...}` 改為 `s.reg.Add(k, &ManagedProcess{...})`。map 讀取 `s.processes[k]` 改為 `s.reg.Get(k)`。這保持封裝，無 escape hatch 寫入。

### 4. manager.go 重寫

```go
// daemon/manager.go
func (s *Server) listAll() []process.ProcessInfo {
    return s.reg.Snapshot()  // 內部已 RLock
}

func (s *Server) findProcesses(target string) []*ManagedProcess {
    return s.reg.FindByTarget(target)  // 內部已 RLock
}

func (s *Server) deleteByName(name string) error {
    targets := s.findProcesses(name)
    if len(targets) == 0 {
        return fmt.Errorf("process or namespace not found: %s", name)
    }
    for _, mp := range targets {
        _ = s.stopProcess(mp)
        s.reg.Remove(mp.Info.Namespace + ":" + mp.Info.Name)
    }
    return nil
}
```

### 5. server.go 關鍵函數改寫

**`startApp`**（lines 218-283）：
- 移除 6 處 `s.mu.Lock/Unlock` pair
- 取代為：
  - `existing, ok := s.reg.Get(key)` 取單一 mp
  - 對 ConfigFile 比對需要迭代 map 的場景，使用 `s.reg.WithRLock(func(m map[string]*ManagedProcess) { ... })` 或 `s.RLock()` + `s.reg.List()`（注意：後者會拷貝 slice，要看情境選擇）
- 採 `s.reg.UpdateInfo(key, func(mp) { mp.Info.X = ... })` 取代寫欄位三步

**`launchProcess`**（lines 285-525）：
- 移除 8 處 `s.mu.Lock/Unlock`
- `existing, ok := s.reg.Get(key)` 取既有 mp
- `for k, mp := range ...` 改為 `s.RLock(); list := s.reg.List(); s.RUnlock(); for i, mp := range list { key := ... }` 或 `s.reg.WithRLock(fn)`
- 寫入用 `s.reg.Add(ns+":"+name, mp)`
- 結尾回傳 `info := mp.Info` 改為 `info := s.reg.GetInfo(key)` 或保持 `s.RLock(); info := mp.Info; s.RUnlock()`

**`watchProcess`**（lines 535-587）：
- 移除 `s.mu.Lock/Unlock` pair
- 改用 `s.reg.UpdateInfo(mp.Info.Namespace+":"+mp.Info.Name, func(mp) { mp.Info.PID = 0; ... })`
- 重啟檢查的 4 步讀+判斷+釋放改為 `s.reg.UpdateInfo(key, func(mp) { ... })` 或在 `s.reg.WithRLock(func(m map[string]*ManagedProcess) { ... })` 中執行

**`stopProcess`**（lines 589-660）：
- 移除 2 處 `s.mu.Lock/Unlock`
- `mp.stopping = true; mp.Info.Status = StatusStopping` 改為 `s.reg.UpdateInfo(key, func(mp) { mp.stopping = true; mp.Info.Status = StatusStopping })`
- 結尾 `mp.Info.Status = StatusStopped; mp.Info.PID = 0` 同上

**`triggerCron`**（lines 764-786）：
- `mp, ok := s.processes[key]` 改為 `s.reg.Get(key)`
- 最後寫 `LastCronAt` / `LastCronStatus` 改為 `s.reg.UpdateCronStatus(key, firedAt, status)`

**`pauseByName`**（lines 716-739）：
- `s.mu.Lock(); mp.paused = true; mp.Info.Status = StatusPaused; s.mu.Unlock()` 改為 `s.reg.UpdateInfo(key, func(mp) { mp.paused = true; mp.Info.Status = StatusPaused })`

**`resumeByName`**（lines 741-767）：
- `s.mu.RLock(); paused := mp.paused; s.mu.RUnlock()` 改為 `s.reg.WithRLock(func(m map[string]*ManagedProcess) { paused := m[key].paused })` 或新增 `s.reg.GetPaused(key)` 輔助方法

### 6. metrics.go 重寫

**`refreshMetrics`**（3 階段）：
- Phase 1（snapshot）：`s.reg.WithRLock(func(m) { for k, mp := range m { targets = append(...) } })` 或 `s.RLock(); targets = ...; s.RUnlock()`
- Phase 2（parallel ps）：不持鎖，無變動
- Phase 3（writeback）：改為 `s.reg.UpdateMetricsWithPIDCheck(key, expectedPID, cpu, mem)` — Registry 內部 Lock + 查 mp + 驗 PID + 寫入

### 7. persistence.go 重寫

**`save`**：
- `s.mu.RLock(); for _, mp := range s.processes { ... }; s.mu.RUnlock()` 改為 `entries := s.reg.SnapshotAppConfigs()` — Registry 內部 RLock + 回傳 `[]process.AppConfig` 切片

### 8. server_test.go 機械式更新

| 模式 | 出現次數 | 取代為 |
|---|---|---|
| `s.mu.RLock()` | 16 | `s.RLock()` |
| `s.mu.RUnlock()` | 15 | `s.RUnlock()` |
| `s.mu.Lock()` | 9 | `s.Lock()` |
| `s.mu.Unlock()` | 6 | `s.Unlock()` |
| `s.processes["k"] = &ManagedProcess{...}` | 12 | `s.reg.Add("k", &ManagedProcess{...})` |
| `mp := s.processes["k"]` 或 `mp, ok := s.processes["k"]` | ~30 | `mp, _ := s.reg.Get("k")` 或 `mp, ok := s.reg.Get("k")` |
| `if _, ok := s.processes["k"]; ok` | ~10 | `if _, ok := s.reg.Get("k"); ok` |
| `for key, mp := range s.processes` | ~3 | `for key, mp := range s.reg.SnapshotMap()` 或 `s.reg.WithRLock(func(m) { ... })` |

> **重點**：測試中所有 31+15+9+6 = **46 處鎖呼叫** 改為 `s.RLock/RUnlock/Lock/Unlock`（無前綴變動，機械式）；所有 **~42 處** `s.processes` 讀寫改為 `s.reg.Add/Get`（小語法變動）。改完後測試**不再需要手動鎖** — 因為 `Add/Get` 內部已持鎖 — 但保留手動鎖呼叫不影響正確性，僅是冗餘。

### 9. README.todo 與 CLAUDE.md 更新

- README.todo: Phase 3 條目更新為 `[x]`，附「結果」段落描述實作細節
- CLAUDE.md: 「Conventions」段第一條原文「All process state is owned by `daemon.Server` behind `sync.RWMutex`」改為「All process state is owned by `daemon/manager.ProcessRegistry` behind its internal `sync.RWMutex`; `daemon.Server` delegates via `s.RLock/RUnlock/Lock/Unlock`」

---

## 關鍵檔案

| 檔案 | 動作 | 行數變化 |
|---|---|---|
| `daemon/process_registry.go` | **新建**：`ProcessRegistry` 結構 + 11 個方法 + escape hatch | +~180 |
| `daemon/manager/registry_test.go` | **新建**：Add/Get/Remove 基本測試 + 並發 race test（TestRegistryConcurrentReadWrite） | +~100 |
| `daemon/server.go` | 替換 2 個欄位 + 6 個 Server-level 委派方法 + 5 個函數內部改寫 | -30 ~ -50 |
| `daemon/manager.go` | listAll/findProcesses/deleteByName 改寫 | -10 |
| `daemon/metrics.go` | refreshMetrics phase 1/3 改寫 | -15 |
| `daemon/persistence.go` | save 改寫 | -5 |
| `daemon/server_test.go` | 機械式更新 88 處 | -40 ~ -60 |
| `README.todo` | Phase 3 標記完成 + 結果段落 | +~10 |
| `CLAUDE.md` | Conventions 第一條更新 | ±2 |

---

## 驗證計畫 (Verification)

```bash
# 編譯與靜態檢查
go vet ./...
go build ./...

# 既有特徵測試（必過）
go test -race -count=2 ./daemon/...
go test -race -count=2 ./...

# 新增 registry 單元測試
go test -race -count=3 ./daemon/manager/...

# 高並行壓測
go test -race -count=5 -run TestHighConcurrencyStartup ./daemon/...

# 檢查無殘留 s.mu 或 s.processes
grep -nE 's\.mu\.|s\.processes' daemon/*.go && echo "FAIL: residual references" || echo "OK: clean"
```

**驗證指標**：
1. `go test -race ./...` 全綠，無 race detector 報告
2. `daemon/manager/registry_test.go` 並發測試穩定通過 3 次
3. Phase 1 寫的 5 個特徵測試（`TestHighConcurrencyStartup`、`TestCronRestartFiresReboot` 等）不退步
4. `grep s\.mu\.|s\.processes` 在 `daemon/` 來源檔為空（測試檔除外，已機械式更新）

---

## 風險與回滾

| 風險 | 嚴重度 | 對策 |
|---|---|---|
| `daemon/manager` 子包 import `daemon` 主包的循環依賴 | 中 | 已規避：`manager` 是 `daemon` 的子包，子包 import 主包是 Go 語言允許的單向依賴。`go build ./...` 驗證。 |
| `UpdateInfo` callback 內若 callback 內部再次呼叫 Registry method 造成遞迴死鎖 | 中 | 註解明確標示「callback 內不得呼叫同 Registry 的其他方法」；callback 應為純欄位 mutate |
| 測試機械式更新漏改造成編譯錯誤 | 低 | 編譯時即會暴露 — `s.mu` 不存在、`s.processes` 不存在 — 整批 grep+sed 一次完成 |
| `WithRLock(fn)` API 對 test 來說語法太重 | 低 | 測試中大多數是 `s.reg.Get(k)`，不需要 `WithRLock`；後者僅在 server.go 內部 1-2 處使用 |
| stopProcess 內 `s.mu.Lock` 移除後 `mp.stopping=true` 與 `watchProcess` 讀 `mp.stopping` 的可見性 | 低 | `UpdateInfo` 內部 `Lock/Unlock` 對 Go memory model 保證 happens-before；watchProcess 後續讀 `mp.stopping` 一定看見 true |

**回滾策略**：每個 commit 獨立。
1. commit 1: 新增 `daemon/process_registry.go` + `registry_test.go`（純新增，不影響現有代碼）
2. commit 2: 修改 `daemon/manager.go` 與 `daemon/persistence.go`（最小變更，僅 `save/listAll/findProcesses`）
3. commit 3: 修改 `daemon/metrics.go` refreshMetrics
4. commit 4: 修改 `daemon/server.go` 5 個函數（最大變更）
5. commit 5: 修改 `daemon/server_test.go` 機械式更新
6. 每個 commit 後 `go test -race ./...` 必須綠燈；任一紅燈 `git reset --hard HEAD~1`

---

## 預估工作量

| 階段 | 工作量 | 風險 |
|---|---|---|
| 新建 `ProcessRegistry` + 測試 | 1 小時 | 低 |
| 遷移 manager.go / persistence.go / metrics.go | 30 分鐘 | 低 |
| 遷移 server.go 5 個函數 | 1.5 小時 | 中 |
| server_test.go 機械式更新 88 處 | 1 小時 | 低（純 sed/Edit） |
| README.todo + CLAUDE.md 更新 | 15 分鐘 | — |
| **總計** | **4-5 小時** | |

---

## 完成後 TODO 動作

- [ ] 標記 README.todo Phase 3 為 `[x]`，加「結果（2026-07-02）」段落
- [ ] 將 `plans/architecture-extract-registry.md` 用 `git mv` 移到 `docs/specs/extract-process-registry.md`（同 Phase 2 模式）
- [ ] 補上 Phase 3 條目的「規格」連結
- [ ] 將 `plans/architecture-daemon-decoupling-phase3.md` 同樣移到 `docs/specs/`
- [ ] 更新 CLAUDE.md Conventions 第一條