# 第四階段：抽離進程執行器 (Extract executor)

## Status

[x] **完成** — 2026-07-03

子包結構（與原計畫一致）：
- `daemon/executor/executor.go`  — `Executor` struct + `Start`/`Watch`/`Stop`
- `daemon/executor/builder.go`   — `BuildCommand`（從 `daemon/builder.go` 搬遷）
- `daemon/executor/watcher.go`   — `NewFileWatcher` 自由函數（從 `daemon/watcher.go` 重構）
- `daemon/executor/metrics.go`   — `MetricsCollector` + `MetricsBackend` interface（從 `daemon/metrics.go` 搬遷）

### 與原計畫的偏離

原計畫擔心 `daemon/executor` 與 `daemon` 之間會因為 `*ManagedProcess` 互相引用而產生循環依賴。實際做法是讓 `Executor` 的 API 只接收**原始型別**（`*exec.Cmd`、`*os.File`、`*fsnotify.Watcher`、`chan struct{}`、`func()`），完全不參考 `*ManagedProcess`。`MetricsBackend` 也改成只暴露 `(key, pid, online)` 的精簡 `ProcessSample` 結構，`ProcessRegistry.SnapshotForMetrics` 提供此介面。這樣 `daemon/executor` 不需 import `daemon`，打破了循環。

`onProcessExit` 的小修正：在 Watch / Stop 競爭 registry lock 時，Stop 的 onStopped 若先搶到鎖，Watch 的 onExit 會把 Status 覆寫回 `StatusErrored`。修正方式是 Watch 只在 `!mp.stopping` 時寫 Status，這樣「stopped」的狀態由 Stop 獨佔。

### 結果

- `daemon/server.go`：772 → 695 行（-77）
- `go test -race ./...` 全綠（包含 `TestStopProcessKillsChildren`、`TestCronRestartFiresReboot`、`TestSaveConcurrentWithMapMutation` 等關鍵回歸測試）

---

## Context（原始設計）

`daemon.Server` 在 `daemon/server.go` 內直接實作了 `launchProcess`/`watchProcess`/`stopProcess` 三個函數，加上 `daemon/builder.go`（48 行 `buildCommand`）、`daemon/watcher.go`（58 行 `startFileWatcher`，是 `Server` 的方法並直接呼叫 `s.restartByName`）、`daemon/metrics.go`（143 行 `refreshMetrics`/`StartMetricsCollector`/`getProcessMetrics`），總計約 250 行的作業系統層邏輯直接耦合在 `Server` 結構上。

來源計畫：
- [plans/architecture-extract-executor.md](../plans/architecture-extract-executor.md)（**主計畫**：4 階段 Strangler-Fig，定義 `daemon/executor/` 子包結構）

**目標**：抽出 `Executor` 結構體封裝單一進程的「衍生 + 等待 + 信號」生命週期作業；`Server` 僅負責 RPC 路由、狀態協調、cron 排程。

**鎖方向約束**（這是設計的靈魂）：
- `Server` 在持有 `ProcessRegistry` 寫鎖時**可以**呼叫 `Executor`（此時 Executor 不持任何鎖）
- `Executor` 內部**絕對不能**持有 `ProcessRegistry` 的鎖
- `Executor` 與 `Server` 的所有互動通過**回呼函式 (callbacks)**：
  - `onStopping(key)` — `Stop` 開始前呼叫，Server 將 `stopping=true`、`Status=StatusStopping`
  - `onStopped(key)` — `Stop` 完成後呼叫，Server 將 `Status=StatusStopped`、`PID=0`
  - `onExit(key, err)` — `Watch` 偵測到進程退出時呼叫，Server 更新狀態 + 決定是否 auto-restart
  - `onFileChanged()` — Watcher 偵測到檔案變更 + debounce 完成時呼叫，Server 觸發 restartByName
- 「Manager 呼叫 Registry 時加鎖，Executor 不持狀態鎖」是 Phase 3 建立的單向依賴原則，Phase 4 必須延續。

---

## 完成後 TODO 動作

- [x] 標記 Phase 4 為 `[x]`（本檔案）
- [ ] 將 `plans/architecture-extract-executor.md` 用 `git mv` 移到 `docs/specs/extract-process-executor.md`
- [ ] 補上 Phase 4 條目的「規格」連結
- [ ] 在 spec 標頭加註位置偏離（子包結構）

---

# 第五階段：抽離網路層 (Extract network)

## Status

[x] **完成** — 2026-07-03

子包結構：
- `daemon/network/listener.go`  — `Listen(socketPath, m Manager)`
- `daemon/network/handler.go`   — `Handle(conn, m Manager)` + 內部 `dispatch(req, m)`
- `daemon/network/manager.go`   — `Manager` interface（11 個方法）

`Server` 透過下列**公開方法**實作 `Manager`（內部 helper 由原本小寫改名為大寫以滿足介面）：
`StartApp`/`StopByName`/`RestartByName`/`PauseByName`/`ResumeByName`/
`DeleteByName`/`ListAll`/`Save`/`Resurrect`/`KillAll`/`Ping`。

`Server.Listen` 變成薄殼委派給 `network.Listen`。

### 關鍵設計

- **單向依賴**：`network` 只 import `model` 與 `process`，**不** import `daemon`。`Server` 透過隱性介面滿足 `Manager`（Go 的 structural interface）。
- **匯入循環防護**：`network/manager.go` 是介面唯一宣告處；`executor` 與 `registry` 都不能 import `network`。
- **`network.Handle` 內部對 `CmdKill` 觸發 `os.Exit`**：與原本 `Server.handleConn` 行為一致。

### 結果

- `daemon/server.go`：695 → ~710 行（+15；介面方法改名 + 加上 `Ping`）
- `daemon/network/`：3 個新檔，共 ~180 行
- `go test -race ./...` 全綠
- E2E smoke 測試（start daemon → send CmdPing / CmdList / bogus command）OK

### 風險與回滾

| 風險 | 對策 |
|---|---|
| 介面方法數量與原本 dispatcher 不對齊 | `Manager` 介面直接對應原 `handleConn` switch case，逐一比對 |
| 並發死鎖（Server 同時持有 registry 鎖並呼叫 Manager 方法） | Manager 方法與 `daemon/server.go` 的 helper 一一對應，不引入新的鎖競爭 |
| `os.Exit` 從 network 觸發破壞測試可控性 | `Handle` 用 150ms 延遲 + goroutine 觸發 `os.Exit`，與原本一致 |

---

# 第六階段（未來）：Manager 拆出為獨立型別

把 `Manager` 從 `*daemon.Server` 抽到一個輕量結構體（持 `*ProcessRegistry` + `*executor.Executor` + `*cron.Scheduler`），`Server` 只負責組裝並持有它。這樣測試可以直接構造 Manager 而不需要走整個 `Server` 生命週期。