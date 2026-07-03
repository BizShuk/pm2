# 架構演進與優化計畫 — extract-network-layer (Phase 5)

> **範圍 (Scope)**
> 第五階段抽離 `daemon.Server` 中混合的 Unix socket 監聽 (`Listen`) 與連線分發 (`handleConn`) 至 `daemon/network/` 子包，僅依賴 `Manager` 介面契約。`executor` 與 `process_registry` 不得引用 `network`，反之亦然 — 形成由外向內的嚴格單向依賴。Commit: `31b36bc refactor(daemon): extract network listener into daemon/network subpackage` (2026-07-03)。

## 1. 現有架構診斷與技術債 (Architecture Diagnosis & Technical Debt)

在抽離前，`daemon.Server` 同時承擔兩組本質迥異的職責：

* `診斷一：Server 同時管網路傳輸與業務邏輯 (Transport and Business Mixed in Server)`
  在 [server.go](../daemon/server.go) 中，`Server.Listen(socketPath)` 綁定 Unix socket、執行 accept 迴圈；`Server.handleConn(conn)` 解析 `model.Request` 並依 `Command` 分派至 11 個業務方法（`StartApp`/`StopByName`/`RestartByName`/`PauseByName`/`ResumeByName`/`DeleteByName`/`ListAll`/`Save`/`Resurrect`/`KillAll`/`Ping`）。伺服器結構體同時持有 socket 監聽迴圈與進程狀態註冊表，違反單一職責原則。

* `診斷二：handler 內聯在 Server 中,難以單獨測試 (Handler Coupled to Server)`
  `handleConn` 直接呼叫 `s.X()` 等業務方法，無法在沒有完整 Server 的前提下進行 RPC dispatch 邏輯的單元測試。若要測試一個錯誤的 `Request` 結構會產生怎樣的 `Response`，必須實例化整個 `Server`、綁定 socket、配置 cron / registry。

* `診斷三：沒有抽象介面,Mock 測試需橋接整個物件 (No Abstraction for RPC Surface)`
  沒有 `Manager` 介面契約，`daemon.network` 與 `daemon` 之間的耦合是具體型別 (`*daemon.Server`)。任何想測試 network 層的測試都必須 import daemon 包，進而拉入 executor / process / cron 等完整依賴鏈。

* `診斷四：CmdKill 退出副作用隱藏在 handler 內 (Hidden Side Effect in Handler)`
  `handleConn` 對 `CmdKill` 的處理包含：寫回 `Response` 後 `go func(){ time.Sleep(150ms); os.Exit(0) }()`。這個副作用與「寫回 Response → 關閉連線」的正常路徑交織，使 socket 生命週期難以推理。

---

## 2. 複雜度量測 (Complexity Metrics)

* `程式碼規模與拆分效益 (Code Size and Refactoring Yield)`
  抽離前 `daemon/server.go` 約 `701` 行；`Listen` (約 20 行) + `handleConn` (約 30 行) + 11 個 case 分派 (約 80 行) 合計約 130 行的網路層邏輯被混雜在業務方法之間。抽出後 `daemon/network/` 總計 `~150` 行，獨立可讀。

* `改動熱點 (Change Hotspots)`
  `daemon/server.go` 在過去 12 個月被改動 `17` 次，其中多次是因為新增 RPC command 而需要：
    1. 在 `handleConn` 加 case 分支
    2. 在 Server 加上對應的業務方法
    3. 在 `model/protocol.go` 加 `Cmd*` 常數
  抽出 `network` 介面後，新增 RPC 只需：(1) 加 `Cmd*` 常數、(2) 在 `Manager` 介面加方法、(3) Server 隱性實作、(4) `dispatch` 加 case。

* `扇入扇出分析 (Fan-in/Fan-out Analysis)`
  抽出前 `daemon` 套件的扇入包含 `cmd/*` (5 個檔案) + `tui/model.go`，但這些依賴其實只需要 `model.SendRequest` (Phase 2 已抽離) + `daemon.NewServer`。抽出 `network` 後，依賴圖更精確：
    - `cmd/*` → `model` (僅 send/recv)
    - `tui/model.go` → `model`
    - `daemon/network` → `model` + 自身的 `Manager` 介面
    - `daemon.Server` 實作 `network.Manager` (隱性)

---

## 3. 架構簡化與解耦設計 (Simplification & Decoupling Design)

我們提出以 `Manager` 介面契約為核心的解耦方案：

* `職責拆分 (Responsibility Segregation)`：
  - `網路傳輸層 (network)`：綁定 Unix socket、accept 迴圈、單連線 RPC 解析/序列化、`CmdKill` 退出副作用。**不持有任何業務狀態、不引用 `daemon` 套件**。
  - `伺服器層 (daemon.Server)`：實作 `network.Manager` 介面的 11 個方法；擁有 `ProcessRegistry` 與 `Executor`；啟動背景協程（metrics、auto-save、auto-resurrect）。
  - `介面契約 (Manager interface)`：定義在 `daemon/network/manager.go`，是 network 層唯一需要的業務抽象。

* `單向依賴約束 (Single-Direction Dependency Invariant)`：
  ```mermaid
  flowchart LR
      CLI["CLI (cmd/*)"] -->|"SendRequest"| Model["model"]
      TUI["TUI (tui/model)"] -->|"SendRequest"| Model
      Server["daemon.Server"] -.->|"隱性實作"| Manager["network.Manager 介面"]
      Network["daemon/network/*"] -->|"僅呼叫"| Manager
      Network --> Model
      Network --> Process["process (僅型別)"]
      Server --> Executor["daemon/executor"]
      Executor -.->|"ProcessInfo only"| Process
  ```
  - `network` → `Manager` 介面（僅），`process`（僅回傳型別）— 永遠不 import `daemon`。
  - `daemon` → `network`（僅呼叫 `network.Listen`），但 executor / process_registry 不引用 network。
  - **這是 Phase 5 的 import-cycle guard**：測試可用 mock `Manager` 注入 network 層，反之亦然。

* `介面契約的選擇 (Why Interface, Not Concrete Type)`：
  Go 的 implicit interface satisfaction 讓 `daemon.Server` 無需宣告「I implement `network.Manager`」 — 只要方法集合匹配即可。這避免了「為了介面而包一個 wrapper struct」的反模式。介面定義於 consumer 端（network 套件）而非 provider 端（daemon 套件），符合 Go 社群的「accept interfaces, return structs」慣例。

* `CmdKill 副作用保留 (Preserve the Side Effect)`：
  為維持重構前的行為對等，`Handle` 在寫回 `Response` 後，針對 `CmdKill` 排程 `go func(){ time.Sleep(150ms); os.Exit(0) }()`。這個副作用**留在 network 層**而不是 Manager 介面，因為它是「socket 層的 cleanup 動作」（確保 response 已被 flush），而非業務邏輯。

---

## 4. 目錄與模組重整方案 (Reorganization Map)

### 抽出後的子包結構

```tree
pm2/
└── daemon/
    ├── server.go             # 實作 network.Manager 介面；Listen 變薄殼委派
    ├── manager.go            # ListAll / DeleteByName / Ping (其餘 Manager 方法直接在 server.go)
    ├── process_registry.go   # ProcessRegistry (Phase 3 已抽離)
    ├── executor/             # Phase 4: 進程生命週期 (Start/Watch/Stop)
    └── network/              # Phase 5: Unix socket + RPC dispatch
        ├── manager.go        # Manager 介面契約 (11 methods)
        ├── listener.go       # Listen(socketPath, m) — bind + accept loop
        └── handler.go        # Handle(conn, m) — 單連線 dispatch + CmdKill hook
```

### 舊模組與新結構之遷移映射表 (Migration Map)

| 舊檔案/方法 | 新模組/方法 | 調整要點 |
| :--- | :--- | :--- |
| `Server.Listen(socketPath)` 內部 socket 綁定 | `network.Listen(socketPath, m)` | 抽出 `os.Remove` + `net.Listen("unix")` + accept 迴圈 |
| `Server.handleConn(conn)` 解析/序列化 | `network.Handle(conn, m)` | 抽出 read JSON / dispatch / write JSON 三步 |
| `Server.handleConn` 內的 switch 11 cases | `network.dispatch(req, m)` (free function) | 獨立成自由函式便於單元測試 |
| `Server.handleConn` 的 `CmdKill` 退出副作用 | `network.Handle` post-response hook | 副作用保留在 network 層（socket cleanup 語意） |
| 隱性假設「`*Server` 滿足業務介面」 | `network.Manager` 介面 | 顯性化契約；Server 隱性滿足 |

### Manager 介面契約

```go
// daemon/network/manager.go
type Manager interface {
    // CmdStart
    StartApp(req *model.AppStartReq) ([]process.ProcessInfo, error)

    // CmdStop / CmdRestart
    StopByName(name string) error
    RestartByName(name string) error

    // CmdPause / CmdResume
    PauseByName(name string) error
    ResumeByName(name string) error

    // CmdDelete
    DeleteByName(name string) error

    // CmdList
    ListAll() []process.ProcessInfo

    // CmdSave / CmdResurrect
    Save() error
    Resurrect() error

    // CmdKill — graceful stop of every managed process
    // (does NOT exit the daemon — Handle's post-response hook
    // schedules os.Exit separately).
    KillAll()

    // CmdPing
    Ping()
}
```

---

## 5. 插件化與可擴充性機制 (Plugin & Extensibility Mechanism)

* `必要性評估 (Necessity Assessment)`
  本專案的 socket 傳輸層主要綁定 Unix domain socket 與 JSON line-delimited 協定。潛在擴充點（gRPC、TCP listener、TLS）目前無明確需求（少於 1 個），引入 plugin 機制屬於過度設計。

* `最簡可行解耦設計 (MVE Design)`
  介面契約即足夠。`Manager` 介面讓 network 層在測試中可注入 mock，模擬 11 種 RPC 回應而無需綁定 socket：
  ```go
  type fakeManager struct {
      startAppFn func(*model.AppStartReq) ([]process.ProcessInfo, error)
      // ... 其他方法對應欄位
  }

  func (f *fakeManager) StartApp(req *model.AppStartReq) ([]process.ProcessInfo, error) {
      return f.startAppFn(req)
  }
  ```
  這使得 `network.Handle` 與 `network.dispatch` 可在沒有真實 Server 的前提下測試錯誤處理路徑（如 unknown command、missing app config、malformed JSON）。

* `未來擴充預留點 (Future Extension Points)`
  若未來需要支援 TCP / TLS / gRPC，只需：
    1. 新增 `network/tcp_listener.go` 呼叫相同 `Handle(conn, m)`（`net.Conn` 介面已統一）
    2. 不需修改 `dispatch` 或 `Manager` 介面
  介面已對傳輸層抽象（`net.Conn`），不對協定抽象 — 故仍維持輕量。

---

## 6. 漸進式重構路徑與驗證 (Refactoring Roadmap & Verification)

完全遵循 `絞殺榕模式 (Strangler-Fig Pattern)`，每一步可獨立編譯、回滾。

### 階段 1：定義 Manager 介面 (Define Manager Interface)

* 步驟 1：建立 `daemon/network/manager.go`，列出 11 個方法簽名（與 `Server` 既有 public 方法一一對應）。
* 步驟 2：暫時保留 `Server.handleConn` 不變；先讓 `*Server` 隱性滿足 `network.Manager`（Go compiler 會在後續引入時強制檢查）。
* 驗證命令：`go build ./daemon/network/...` 通過。

### 階段 2：抽出 listener.go (Extract Listener)

* 步驟 1：將 `os.Remove(socketPath)` + `net.Listen("unix", socketPath)` + accept 迴圈遷移至 `daemon/network/listener.go::Listen(socketPath, m)`。
* 步驟 2：`Server.Listen` 變成僅呼叫 `s.StartMetricsCollector()` + 啟動背景協程 + `return network.Listen(socketPath, s)`。
* 驗證命令：`go build ./...` 通過，`go test -race ./...` 全綠。

### 階段 3：抽出 handler.go (Extract Handler)

* 步驟 1：將 `handleConn` 的 read / dispatch / write 三段遷移至 `daemon/network/handler.go::Handle(conn, m)`。
* 步驟 2：switch 11 cases 改寫為 `dispatch(req, m)` 自由函式（避免 Handle 函式過長）。
* 步驟 3：`CmdKill` 的 `os.Exit` 副作用保留在 `Handle` 的 post-response hook。
* 驗證命令：`go test -race ./...` 全綠；`pm2 list` / `pm2 start` / `pm2 kill` E2E 行為與重構前一致。

### 階段 4：介面契約檢查 (Interface Contract Audit)

* 步驟 1：執行 `go list -deps ./daemon/network/... | grep 'pm2/daemon$'` 確認 network 不 import daemon。
* 步驟 2：執行 `go list -deps ./daemon/executor/... | grep 'pm2/daemon$'` 確認 executor 不 import daemon。
* 步驟 3：執行 `go list -deps ./daemon/network/... | grep 'pm2/cmd'` 確認 network 不被 CLI 繞過。
* 驗證命令：以上 grep 結果皆為空（exit code 1，無匹配）。

---

## 7. 風險與回滾策略 (Risks & Rollback)

* `介面契約漂移風險 (Interface Drift)`：
  - `問題`：日後若有人修改 `Server` 的某個方法簽名（例如 `StartApp` 從 `([]ProcessInfo, error)` 改為 `(error)`），Go compiler 不會主動提示「你破壞了 `network.Manager` 契約」 — 隱性介面滿足只檢查方法集合，不檢查語意。
  - `對策`：每個 Manager 方法的 doc-comment 明確標註「`// Satisfies network.Manager (CmdX)`」，並在 `server.go` 的方法定義處保留對應註解。新增方法時，雙向同步 (Manager 介面 + Server 實作 + dispatch case)。

* `RPC 協定不相容風險 (Protocol Incompatibility)`：
  - `問題`：若 `network.dispatch` 不慎改了 JSON 欄位（例如把 `model.Request.Command` 從字串改為 int），將導致舊版 CLI/TUI 與新版 daemon 無法通訊。
  - `對策`：`model/protocol.go` 在 Phase 2 已抽離為獨立套件，network 僅 `import "github.com/bizshuk/pm2/model"`，不修改 model 的 JSON tag。重構前後保留相同的 `Request`/`Response`/`AppStartReq` struct。

* `CmdKill 副作用時序風險 (CmdKill Side Effect Timing)`：
  - `問題`：`Handle` 在 write Response 後 `go func(){ time.Sleep(150ms); os.Exit(0) }()`，若 150ms 內有其他連線進入，可能被一併中斷。
  - `對策`：保留原始 150ms 延遲（這是 Phase 5 之前的既有行為）；若日後發現問題，可改用 `sync.WaitGroup` 等連線級別的退出協調，但不在本階段範圍。

* `回滾策略 (Rollback Strategy)`：
  - 每個階段（介面定義 / listener 抽出 / handler 抽出）均為獨立 commit，可逐個 `git revert`。
  - 若 `go test -race ./...` 出現紅燈且 30 分鐘內無法定位，立即 `git reset --hard HEAD~1` 回滾至上一穩定狀態。
  - 由於介面為隱性滿足，介面定義階段（階段 1）的 revert 不影響後續階段的進行，可隨時安全回滾。