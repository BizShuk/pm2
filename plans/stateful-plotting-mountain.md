# 守護進程解耦重構 — 5-Phase Strangler-Fig Plan

## Context

`daemon/` 目前是扁平結構（9 個檔案 2514 行），`daemon.Server` 同時承擔網路監聽、RPC 路由、進程生命週期、cron 整合、metrics 採集、檔案監看 — 是典型的「上帝物件」（670 行 `server.go`）。`cmd/` 與 `tui/` 共 6 個檔案 import `daemon` 整個套件，僅用到 `Request`/`Response`/`Cmd*`/`SendRequest` 等純通訊型別，但被迫編譯整個 daemon 程式碼。

**目標**：將 `daemon/` 拆為 `model/` + `daemon/{manager,executor,network}/` + `tui/views/`，讓 `cmd` 與 `tui` 只依賴最小的 `model` 套件（純型別、無 daemon 業務邏輯）。採 strangler-fig 漸進式，每個 phase 結束時都維持可編譯、可測試、可手動 E2E 驗證。

**Phase 1（特徵測試）已完成**（2026-06-30）：5 個 characterization tests + 修 `server.go:499` race，全專案 `go test -race ./...` 綠燈。

## 執行策略

使用者確認：**一次做一個 phase**，每個 phase 結束時回報驗證結果，等使用者決定是否進下一個。**Hard cut** — 不留 shim 雙重來源。

每個 phase 的結尾都會：
1. `go vet ./... && go build ./...` 通過
2. 對應的 verification 指令通過（見各 phase 下方）
3. `go test -race ./...` 全綠（除了該 phase 範圍外的少量既有 flaky，會明確標出）

---

## Phase 2：抽離 RPC 協定包（Extract protocol package）

### 範圍
- 新建 `model/protocol.go` 與 `model/protocol_test.go`（basic round-trip 測試）
- 從 `daemon/protocol.go` 搬移 `CommandType`、`Request`、`Response`、`AppStartReq`、`WriteJSON`、`ReadJSON`、`Dial`、`SendRequest` 到 `model/`
- **刪除** `daemon/protocol.go`（hard cut，無 shim）
- `daemon/server.go` 內部把 `Request`/`Response`/`Cmd*` 改 import `model`
- `cmd/{daemon,monitor,logs,start,stop}.go` 與 `tui/model.go` 改 import `model`，替換 `daemon.Request` → `model.Request` 等
- `daemon/manager.go` 內 `AppStartReq` 改 import `model`

### 為什麼不拆 `process.ProcessInfo` / `DumpEntry` 進 `model`
- 雖然 `AppStartReq` 引用了 `process.ProcessInfo` 的部分概念，但目前沒有實際 import — 它的欄位是純字串/數字/切片，可直接搬移。
- `process` 套件是純資料型別（無業務邏輯），已經是 leaf，保留在 `process/` 即可。

### 變更清單
| 檔案 | 動作 |
|---|---|
| `model/protocol.go` | **新建**：搬移 `CommandType` 常數、`Request`/`Response`/`AppStartReq` structs、`WriteJSON`/`ReadJSON`/`Dial`/`SendRequest` 函式 |
| `model/protocol_test.go` | **新建**：round-trip encode/decode + SendRequest 連線錯誤處理測試 |
| `daemon/protocol.go` | **刪除** |
| `daemon/server.go` | import 改 `model`；`Request`/`Response`/`Cmd*` 換前綴；`AppStartReq` 換前綴 |
| `daemon/manager.go` | `AppStartReq` 換前綴 |
| `cmd/{daemon,monitor,logs,start,stop}.go` | import 改 `model`；`daemon.Request` → `model.Request` 等 |
| `tui/model.go` | import 改 `model`；`daemon.Request` → `model.Request` 等 |
| `cmd/eco_install*.go`、`cmd/eco_wizard.go` 等 | 確認無 `daemon.Request` 引用（grep 過應無） |

### JSON tag 一致性保證
- `model/protocol.go` 內的 `Request`、`Response`、`AppStartReq` 必須**完全照抄** `daemon/protocol.go` 的 json tag，**不得修改大小寫、omitempty、欄位順序以外的格式**。
- 新增 `model/protocol_test.go` 內會有 JSON round-trip 測試，鎖定 tag 行為。
- 既有 RPC 行為由 `TestBaseEnvSurvivesRestartAndResurrect` 隱含驗證（save/resurrect 仍需正確序列化）。

### Verification
```bash
go vet ./... && go build ./...
go test -race -run TestProtocol ./model/...           # 新增
go test ./cmd/... ./tui/...                            # 驗證 cmd/tui 仍能編譯呼叫 daemon
go test -race ./...                                    # 整專案不退步
```

### 風險
- **低**。純檔案搬移 + import 替換，無邏輯改動。
- 既有 RPC 序列化行為被既有測試間接驗證（`TestBaseEnvSurvivesRestartAndResurrect` 等）。

### 預估工作量
~30-50 行新檔案 + ~30 處 import/前綴替換。**約 20-30 分鐘**。

---

## Phase 3：抽離進程註冊表（Extract ProcessRegistry）

### 範圍
- 新建 `daemon/manager/registry.go`，定義 `Registry` struct，封裝 `map[string]*ManagedProcess` + `sync.RWMutex`
- 提供 methods：`Add(key, *ManagedProcess)`、`Get(key) *ManagedProcess`、`Remove(key)`、`List() []*ManagedProcess`、`FindByTarget(target) []*ManagedProcess`、`Snapshot() []process.ProcessInfo`、`Len() int`
- `daemon/manager.go` 內的 `listAll`、`findProcesses`、`deleteByName` 改為轉發到 `Registry` methods
- `daemon/server.go` 內所有 `s.processes` 直接讀寫（13 處）改為 `s.reg.Add/Get/Remove/List`
- 移除 `Server` struct 內的 `processes` 與 `mu` 兩個直接欄位
- **保留** `Server.mu` 對外暴露（很多測試用 `s.mu.RLock()` 直接讀 `s.processes`），但內部改為 `s.reg.mu()` 委派

### 為什麼這個設計
- **單一職責**：Registry 只管 thread-safe map 存取，不做 I/O、不做 I/O 阻塞操作。
- **保留 `Server.mu` 介面**：daemon/server_test.go 有 30+ 處直接 `s.mu.RLock()` 讀 `s.processes[key]`，改 method 化要全改測試。
- **方法化 lock 取得**：`Registry.mu()` / `RLock()` / `RUnlock()` / `Lock()` / `Unlock()` 提供 escape hatch 給測試。

### 變更清單
| 檔案 | 動作 |
|---|---|
| `daemon/manager/registry.go` | **新建**：`Registry` struct + `Add/Get/Remove/List/FindByTarget/Snapshot/Len` + `mu/RLock/RUnlock/Lock/Unlock` escape hatches |
| `daemon/manager/registry_test.go` | **新建**：基本 Add/Get/Remove 測試 + 並發讀寫 race test |
| `daemon/manager.go` | `listAll`/`findProcesses`/`deleteByName` 改為呼叫 `s.reg.*`；保留檔案（不刪除以便對照） |
| `daemon/server.go` | `Server.processes`/`mu` 改為 `Server.reg`；13 處 `s.processes`/`s.mu.*` 改為 `s.reg.*` |
| `daemon/server_test.go` | 30+ 處 `s.mu.RLock()` 改為 `s.reg.RLock()`；`s.processes[key]` 改為 `s.reg.Get(key)`（或保留逃生口） |

### 鎖方向約束
- **唯一持鎖者**：`Registry` 內的 `sync.RWMutex`。所有 method 都在內部 `RLock`/`Lock` 與 `RUnlock`/`Unlock`。
- **呼叫者不可在持鎖時呼叫其他 Registry method**（避免遞迴死鎖），用註解說明。
- Phase 4 會引入 Executor，屆時 Executor **不持** Registry 鎖 — 由 Manager 層協調。

### Verification
```bash
go test -race ./daemon/manager/...                     # 新增 registry 測試
go test -race ./daemon/...                             # 既有 server_test 全綠
go test -race ./...                                    # 整專案不退步
```

### 風險
- **中**。改 `s.mu` → `s.reg.mu` 是機械替換，但若漏改一處會 deadlock。
- 既有測試有 30+ 處直接用 `s.mu.RLock()` 讀 `s.processes[key]`，需要全部機械替換。
- Phase 1 寫的 5 個特徵測試會順便當迴歸檢查點（高並行、cron restart、auto-restart 等都觸碰 Registry）。

### 預估工作量
~150 行新檔案 + ~50 處替換。**約 1-2 小時**。

---

## Phase 4：抽離進程執行器（Extract executor）

### 範圍
- 新建 `daemon/executor/` 目錄
- 搬移：
  - `builder.go` → `daemon/executor/builder.go`
  - `watcher.go` → `daemon/executor/watcher.go`
  - `metrics.go` → `daemon/executor/metrics.go`
  - `launchProcess`/`watchProcess`/`stopProcess` 從 `daemon/server.go` → `daemon/executor/lifecycle.go`
- 定義 `Executor` struct 與 `Executor` interface：
  ```go
  type Executor struct {
      homeDir string
      sched   *cron.Scheduler
  }
  type Executor interface {
      Launch(req *model.AppStartReq) (process.ProcessInfo, *ManagedProcess, error)
      Stop(mp *ManagedProcess) error
  }
  ```
- `daemon/Server` 持有一個 `Executor` 實例；`Server.startApp`/`stopByName`/`restartByName`/`triggerCron` 改為呼叫 executor
- **鎖方向約束**：Executor 內不持 Registry 鎖。`Server.launchProcess` 的 map 寫入仍由 Server（Manager）層持 Registry 鎖完成；executor 只負責 build cmd + start process，回傳結果讓 Manager 插入 map

### 變更清單
| 檔案 | 動作 |
|---|---|
| `daemon/executor/builder.go` | **搬移**自 `daemon/builder.go` |
| `daemon/executor/watcher.go` | **搬移**自 `daemon/watcher.go` |
| `daemon/executor/metrics.go` | **搬移**自 `daemon/metrics.go` |
| `daemon/executor/lifecycle.go` | **新建**：`Launch` / `Stop` / `Watch` methods |
| `daemon/executor/executor_test.go` | **新建**：Launch/Stop unit tests（可 mock 出 process） |
| `daemon/server.go` | 移除 `launchProcess`/`watchProcess`/`stopProcess`，改持 `Executor` 委派 |
| `daemon/server_test.go` | 既有測試若直接呼叫 `s.launchProcess` 需更新呼叫方式，或保留薄包裝 |

### 鎖方向
- **Manager**（Server）持 Registry 鎖做 map 寫入
- **Executor** 只做 process lifecycle（fork+exec、signal、wait），不持 Registry 鎖
- Executor 回傳 `*ManagedProcess` 給 Manager，由 Manager 決定插入時機與鎖

### Verification
```bash
go test -race ./daemon/executor/...                    # 新增 executor 測試
go test -race ./daemon/...                             # 既有 server_test 全綠
go test -race ./...                                    # 整專案不退步
# E2E 手動：
pm2 start /tmp/test.js && pm2 list && pm2 stop test   # 確認 CLI 行為一致
```

### 風險
- **中高**。Process lifecycle 是核心業務邏輯，抽離要確保：
  - process 群組信號傳遞（`Setpgid` + `syscall.Kill(-pid, SIGTERM)`）— 對應到「修復孤兒進程」診斷 1.3
  - log file fd 不洩漏
  - auto-restart 的 RestartDelay 與 mp 物件生命週期一致
- Phase 1 的 `TestCronRestartFiresReboot` 會抓 cron callback 與 launchProcess 的生命週期是否一致。

### 預估工作量
~300 行新檔案 + ~200 行 server.go 精簡。**約 2-3 小時**。

---

## Phase 5：抽離網路傳輸層（Extract network layer）

### 範圍
- 新建 `daemon/network/` 目錄
- 搬移：
  - `Listen` → `daemon/network/listener.go`
  - `handleConn` → `daemon/network/handler.go`
- 定義 `Handler` interface 給 Server 實作：
  ```go
  type Handler interface {
      Start(*model.AppStartReq) ([]process.ProcessInfo, error)
      Stop(name string) error
      Restart(name string) error
      Delete(name string) error
      List() []process.ProcessInfo
      Save() error
      Resurrect() error
      Kill()
  }
  ```
- `daemon/Server` 實作 `Handler`（已自然滿足）
- `daemon/network.Handler` struct 接受 `Handler` 介面與 socket path

### 由外向內單向依賴
```
network/   → Manager (handler interface)
manager/   → Executor + Registry
executor/  → cron, process
model/     → (no internal deps)
```

`executor/` 與 `manager/` 不得 import `network/`。驗證方法：`go list -deps ./daemon/manager/... | grep network` 應為空。

### 變更清單
| 檔案 | 動作 |
|---|---|
| `daemon/network/listener.go` | **新建**：`Listen(socketPath string, h Handler) error` |
| `daemon/network/handler.go` | **新建**：`handleConn(conn net.Conn, h Handler)`（從 server.go 搬移並改為介面呼叫） |
| `daemon/network/network_test.go` | **新建**：mock handler + 啟動 listener 測 request/response 流程 |
| `daemon/server.go` | 移除 `Listen`/`handleConn`，新增 `Start(socketPath) error` 委派給 `network.Listen(s, s)` |
| `daemon/cmd/daemon.go` | （無變更，但確保 `daemon.NewServer` 呼叫方式一致） |

### Verification
```bash
go test -race ./daemon/network/...                     # 新增 network 測試
go test -race ./...                                    # 整專案不退步
# import cycle 檢查：
go list -deps ./daemon/manager/... | grep -c network  # 應為 0
go list -deps ./daemon/executor/... | grep -c network  # 應為 0
```

### 風險
- **中**。網路層獨立性高，但 `handleConn` 與 `Server` 的 method 簽名耦合（method 接受 `req.App` 直接 struct 指標）。改成介面呼叫時 method 簽名不變，但需驗證 RPC round-trip 行為不變。
- `TestBaseEnvSurvivesRestartAndResurrect` 隱含測試 RPC 路徑。

### 預估工作量
~200 行新檔案 + ~100 行 server.go 精簡。**約 1-2 小時**。

---

## Phase 6：解耦 TUI 視圖（Decouple TUI views）

### 範圍
- 新建 `tui/views/` 目錄
- 搬移 `tui/renderer.go` 的渲染函式到：
  - `tui/views/list.go`：`buildListTUI`（list view 的主渲染）
  - `tui/views/detail.go`：`buildDetail` + `buildLogs`（detail 與 log 區塊）
  - `tui/views/common.go`：`buildTitle`、`buildFooter`、`drawBorder`、`sepLine`、`getColVal`（共用）
- `tui/model.go` 的 `View()` 改為呼叫 `views.RenderList(m)` / `views.RenderDetail(m, w, h)`
- `tui/renderer.go` 可保留（向後相容的 thin shim）或刪除（hard cut）

### 變更清單
| 檔案 | 動作 |
|---|---|
| `tui/views/list.go` | **新建**：`RenderList(m Model) string`（含 buildLeft + buildListTUI） |
| `tui/views/detail.go` | **新建**：`RenderDetail(m Model, w, h int) string`（含 buildRight + buildDetail + buildLogs） |
| `tui/views/common.go` | **新建**：title、footer、border、column helpers |
| `tui/model.go` | `View()` 改為委派 `views.RenderList(m)` / `views.RenderDetail(m, w, h)`；移除 7 個 build* 方法 |
| `tui/renderer.go` | **刪除**（hard cut） |
| `tui/model_test.go` | 既有測試不變（驗證 Model 的 state 邏輯） |

### 為什麼不在 tui/views_test.go 加新測試
- 渲染測試本質是「給定 state 輸出固定 string」，脆弱且低 ROI。
- Phase 1 的 5 個特徵測試在 `-race` 下全綠就足以驗證 view 拆分沒破壞底層邏輯。
- E2E 手動驗證：`pm2 monit` 對照渲染結果。

### Verification
```bash
go vet ./... && go build ./...
go test -race ./tui/...                                # 既有 model_test 全綠
go test -race ./...                                    # 整專案不退步
# E2E 手動：
pm2 monit                                              # 確認 list/detail 渲染與重構前一致
```

### 風險
- **低**。純 UI 程式碼搬移，無業務邏輯。
- 主要風險是 lipgloss 樣式常數（`clOnline`、`clBorder` 等）需要從 `model.go` 搬到 `views/common.go` 並 export，確保兩邊都能用。

### 預估工作量
~50 行新檔案 + 既有 350 行 renderer.go 重新分配。**約 1 小時**。

---

## 進度追蹤

- [x] Phase 1：特徵測試補強（已完成 2026-06-30，commit 待用戶確認後提交）
- [ ] **Phase 2：抽離 RPC 協定包** ← 下一步
- [ ] Phase 3：抽離進程註冊表
- [ ] Phase 4：抽離進程執行器
- [ ] Phase 5：抽離網路傳輸層
- [ ] Phase 6：解耦 TUI 視圖

## 整體時程預估

| Phase | 工作量 | 風險 |
|---|---|---|
| 2 | 20-30 分鐘 | 低 |
| 3 | 1-2 小時 | 中 |
| 4 | 2-3 小時 | 中高 |
| 5 | 1-2 小時 | 中 |
| 6 | 1 小時 | 低 |
| **總計** | **5-9 小時** | |

## 關鍵設計決策

1. **不用 shim**：Hard cut 避免雙重來源造成「改一邊忘了另一邊」的維護負擔。
2. **保留測試的 escape hatch**：`Registry.mu()` / `s.reg.Get(key)` 讓 daemon/server_test.go 的 30+ 處 `s.mu.RLock()` 不必全部改寫。
3. **Executor 不持 Registry 鎖**：避免 Manager → Registry → Executor → Registry 的循環等待死鎖。
4. **TUI views 不加新測試**：渲染測試脆弱且低 ROI，靠既有 model_test + E2E 視覺驗證。
5. **每 phase 結束的 verification 都跑全專案 `-race`**：防止某個 phase 漏改導致後續 phase 在錯誤基礎上疊加。
