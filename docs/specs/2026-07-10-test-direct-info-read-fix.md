# 測試碼直接讀取 `mp.Info.*` 違反 UpdateInfo 不變量 — 追蹤計畫

> **發現日期**:2026-07-10
> **發現者**:`go test -race -count=1 ./...` 全專案 race test(`TestPauseResumeRunningProcess`)
> **嚴重性**:中 — 測試碼 race,生產碼不受影響,但違反 CLAUDE.md §Conventions invariant
> **不阻擋合併**:refactor 計畫「已完成」宣告仍然成立,本檔為**獨立追蹤**

## Context

`architecture-pm2-refactor.md` 的「驗證」階段(2026-07-10)執行 `go test -race -count=1 ./...` 時,daemon 子套件揭露一個 race:

```
WARNING: DATA RACE
Write at 0x00c0000cc660 by goroutine 335:
  github.com/bizshuk/pm2/daemon.(*ProcessManager).onProcessExit.func1()
      daemon/process_manager.go:473
  github.com/bizshuk/pm2/daemon.(*ProcessRegistry).UpdateInfo()
      daemon/process_registry.go:238
  github.com/bizshuk/pm2/daemon.(*ProcessManager).onProcessExit()
      daemon/process_manager.go:472
  github.com/bizshuk/pm2/daemon.(*ProcessManager).launchProcess.func2()
      daemon/process_manager.go:434
  github.com/bizshuk/pm2/daemon/executor.(*Executor).Watch()
      daemon/executor/executor.go:224

Previous read at 0x00c0000cc660 by goroutine 333:
  github.com/bizshuk/pm2/daemon.TestPauseResumeRunningProcess()
      daemon/server_test.go:1648
```

**Race 本質**:
- 寫入側:`onProcessExit` 透過 `UpdateInfo(key, fn)` 在 registry 寫鎖下設定 `mp.Info.PID = 0`(daemon/process_manager.go:473)
- 讀取側:`TestPauseResumeRunningProcess` 在 daemon/server_test.go:1648 直接讀 `mp.Info.PID`,**未持任何鎖**

CLAUDE.md §Conventions 已明文規定:

> For atomic field mutations on a single `*ManagedProcess`, use
> `pm.reg.UpdateInfo(key, func(mp *ManagedProcess) { ... })` — never mutate
> `mp.Info` fields directly from outside the registry. Direct mutation
> races with `onProcessExit`'s own `UpdateInfo` calls and trips the race
> detector (this is what `TestSaveConcurrentWithMapMutation` was originally
> designed to catch).

雖然該段措辭偏「寫入」側,但語意同樣適用於**讀取** — 直接讀 `mp.Info.X` 與 `onProcessExit` 透過 `UpdateInfo` 寫 `mp.Info.X` 並發,仍構成 race。當前測試碼 79 處直接讀取皆為違規。

## 違規位置清單

`daemon/server_test.go` 共 79 處直接 `mp.Info.*` 讀取,分佈於以下測試:

| 測試函數 (推測) | 行號 | 違規欄位 |
| :--- | :--- | :--- |
| `TestStartApp*` | 110, 121 | `Info.BaseEnv` |
| `TestStopProcess*` | 339, 340, 342, 343 | `Info.Status`, `Info.PID` |
| `TestLookupDuplicateStartApp*` | 408, 409, 412, 413 | `Info.ID`, `Info.ConfigFile` |
| `TestRestartPreservesID*` | 449 | `Info.Status` |
| `TestRestartPreservesRestarts*` | 510, 511 | `Info.Restarts` |
| `TestMultipleRestart*` | 621 | `Info.Version` |
| `TestMetricsRefresh*` | 779, 780, 782, 783, 791, 809, 842, 844, 846, 847, 917, 919 | `Info.CPU/Memory/PID` |
| `TestHighConcurrencyStartup*` | 995, 999, 1000, 1002, 1003, 1004, 1006, 1007, 1008, 1054-1056 | `Info.PID/ID/Status/Restarts` |
| `TestCronRestart*` | 1207, 1208 | `Info.Status/PID` |
| `TestPauseResumeRunningProcess` | 1644-1660 | `Info.Status/PID`(**目前 race 觸發點**) |

(完整清單見 `grep -n 'mp\.Info\.' daemon/server_test.go`)

## 違規的兩種語意

### A. 單純讀快照(不期待 process 期間欄位變動)

例如:`mp.Info.ID != 42` 驗證 ID 在 restart 後保留。`ID` 一旦指派後即不變,理論上無 race;但仍違反「讀取必須走 lock」invariant。

**修法**:透過 `SnapshotOne` API(見下)讀取值副本,或將測試改為「先 stop → 等 exit → 讀 snapshot」。

### B. 與 background goroutine 並發讀(真正有 race)

例如:`TestPauseResumeRunningProcess` 在 pause/resume 之間讀 `PID`,而 `onProcessExit` 正在背景 goroutine 寫 `PID=0`。

**修法**:必須透過 `UpdateInfo` 同步讀取,或使用 `SnapshotOne` 取值快照(取決於該 API 是否在 registry 鎖下拷貝 — 必須是)。

## 設計:`ProcessRegistry.SnapshotOne` API

新增方法:

```go
// SnapshotOne returns a value-copy of the ProcessInfo stored under key.
// The copy is taken under the read lock so the snapshot is atomic
// with respect to any UpdateInfo / UpdateMetrics / UpdateCronStatus
// write. Callers may freely read fields from the returned value
// without holding any registry lock.
//
// Returns (zero, false) if the key is absent.
//
// Intended for test code and the rare read-only consumer that needs a
// point-in-time ProcessInfo without the cost of Snapshot()'s full
// map copy.
func (r *ProcessRegistry) SnapshotOne(key string) (process.ProcessInfo, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    mp, ok := r.processes[key]
    if !ok {
        return process.ProcessInfo{}, false
    }
    return mp.Info, true
}
```

**為何值型別而非指標**:`process.ProcessInfo` 是純資料結構(內嵌 `process.AppConfig` + 9 個 runtime 欄位),無方法,值傳遞成本 < 200 bytes,且天然隔離寫入 — 測試拿到副本後即使背景 goroutine 繼續更新原 `mp.Info`,副本不受影響。

**為何與 `Get` 並存**:
- `Get` 回傳 live `*ManagedProcess` 指標,適用於「需要持續觀察 + 觸發 stop/restart」的 hot path(例如 cron callback 內的 `pm.reg.Get(key)`)
- `SnapshotOne` 回傳值副本,適用於「讀一個 state,做斷言」的測試場景

## 落地步驟

### 步驟 1:實作 `SnapshotOne`(`daemon/process_registry.go`)

- 在 `Get` 方法後新增 `SnapshotOne` 方法
- 加入 doc comment 說明值型別 + RLock 語意
- 在 `process_registry_test.go` 加單元測試:
  - 並發寫入時值副本不變
  - 缺席 key 回傳 `(zero, false)`
  - 內嵌 `AppConfig` 欄位被正確深拷貝(`LogFile` slice 指標不共享)

### 步驟 2:測試碼轉換(`daemon/server_test.go`)

機械式替換:
- `pm.reg.Get(key)` 然後讀 `mp.Info.X` → `pm.reg.SnapshotOne(key)` 直接讀 `info.X`
- 79 處違規點逐個審查,區分「A. 快照即可」與「B. 必須走 UpdateInfo」
- 對「B.」場景(目前只有 `TestPauseResumeRunningProcess` 一處)改用 `pm.reg.UpdateInfo(key, func(mp *ManagedProcess) { ... })` 包裹讀取

### 步驟 3:`go test -race -count=3 ./...` 全綠

- 三次連跑 + 全專案 race test 無任何 race 警告
- 計入 CI(`make test` 或 `.github/workflows/ci.yml`)

### 步驟 4:更新 CLAUDE.md §Conventions

在「`UpdateInfo` 優於直接 `mp.Info` 寫入」段落加註:

> 讀取亦同:測試碼與 hot path 之外的所有 consumer,優先使用
> `pm.reg.SnapshotOne(key)` 取得 `ProcessInfo` 值副本;
> 只有需要觸發 stop/restart/UpdateInfo 的 hot path 才使用
> `pm.reg.Get(key)` 取得 live `*ManagedProcess` 指標。

## 風險與假設

- **效能影響**:`SnapshotOne` 在 RLock 下做一次 `mp.Info` 值拷貝,O(欄位數);對 79 處測試讀取影響 < 1ms 總計,可忽略。
- **API 表面膨脹**:Registry 從 14 個 method 增為 15 個,屬合理範圍。
- **指標讀取**:`executor.MetricsCollector` 已有自己的 `SnapshotForMetrics` 路徑,不受影響。
- **回滾成本**:`SnapshotOne` 是純新增 API;若發現設計問題,刪除即可,所有測試可改回 `Get` + 鎖(但需手動加 `UpdateInfo` 守衛)。

## 觸發時機

- **建議**:緊接 refactor 計畫合併後下一次 minor bump 前完成(因為本檔揭露的 race 會被 `go test -race -count=N ./...`(N ≥ 2)重現,雖不阻擋當前合併但屬「已知問題」)。
- **可延後**:若短期內無新增 process lifecycle 邏輯,且 CI 未跑 `-count=3`,可延至下一輪 refactor 週期。

## 驗證指令

```bash
# 確認 race 在新設計下消失
go test -race -count=3 ./daemon/...

# 確認 API 行為正確
go test -race -count=1 -run TestRegistrySnapshotOne ./daemon/

# 確認無新增 import cycle
go list -deps ./daemon | grep -E 'bizshuk/pm2/' | sort
```

## 結果(2026-07-10)

- **API**:`daemon/process_registry.go` 於 `Get` 之後新增 `SnapshotOne(key string) (process.ProcessInfo, bool)`,在 RLock 下回傳 `mp.Info` 值副本,缺席回 `(zero, false)`。doc comment 點明其為 `UpdateInfo` 的讀側對應、CLAUDE.md §Conventions 認可的讀取路徑。
- **單元測試**:`daemon/process_registry_test.go` 新增 `TestRegistrySnapshotOne`,涵蓋:
    - 缺席 key 回 `(zero, false)`
    - 值副本隔離 live mutation(後續 `UpdateInfo` 寫入不影響已取的 snapshot)
    - `BaseEnv` slice / `Env` map 深拷貝(呼叫端 append 不會污染 live entry)
    - 並發讀寫 1000×1000 在 `-race` 下通過
- **測試碼替換**:`daemon/server_test.go` 79 處 `mp.Info.*` 直接讀取全數替換:
    - **Category A**(單純讀快照,如 `TestConfigFileReplacement`、`TestRestartsInheritance`、metrics 3 個測試的迴圈、`TestDeleteDuringRestartSleep`):改用 `pm.reg.SnapshotOne(key)` 讀值副本
    - **Category B**(必須與 `onProcessExit` 背景 goroutine 同步,即 `TestPauseResumeRunningProcess` 的 4 處):改用 `pm.reg.UpdateInfo(key, fn)` 閉包讀取,序列化於 registry 寫鎖下
    - **整表迭代**(`TestFindProcesses`、`TestKillAllStopsEveryProcess`、`TestHighConcurrencyStartup`):改走 `Snapshot()` 值副本迭代,不再 `SnapshotMap()` + 裸讀 live 指標
    - 新增測試 helper `pauseState(pm, key) (status, paused, ok)`,原子讀 `(Status, paused)` 供 `TestPauseResumeCronTask` / `TestPausedCronTaskSurvivesResurrect` 使用(因 `paused` 為 `ManagedProcess` 私有欄位,`SnapshotOne` 看不到)
    - `TestRefreshMetricsSkipsRestartedProcess` 的 stub 由 `pm.RLock()` 下裸寫 `mp.Info.PID = 5678` 改為 `pm.reg.UpdateInfo` 寫路徑(寫側亦合規)
    - `TestConcurrentRestartDoesNotRaceOnMpInfo` goroutine B 的 19 處裸讀改為 `SnapshotOne` 值副本欄位讀取,保留其「並發寫入時讀欄位不 race」的回歸意圖
- **CLAUDE.md §Conventions**:在「`UpdateInfo` 優於直接 `mp.Info` 寫入」段落後新增「讀取亦同」段落,並把 `SnapshotOne` 列入高階方法清單。
- **驗證**:`go test -race -count=3 ./...`(cmd / config / config/wizard / daemon / model / tui / tui/views 全部)全綠,無任何 race warning;`go list -deps ./daemon` 無新增 import cycle。
