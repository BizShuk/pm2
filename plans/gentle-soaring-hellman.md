# `pm2 daemon status` — 實作計畫

## Context

`pm2` 目前提供 `pm2 daemon start` 與 `pm2 daemon kill`，但沒有「目前 daemon 狀態如何」的查詢指令。
使用者想知道的是：

- daemon 是否真的在跑（socket 是否活著）
- 跑起來的 daemon 的 PID、啟動時間、版本、組態路徑
- 它管轄幾個 process

目前要回答這類問題得自己 `lsof` socket、翻 `~/.pm2/daemon.log`、或掃 `~/.pm2/dump.json` —
對日常操作太繁瑣。本次新增 `pm2 daemon status`，把這些資訊透過既有的 RPC 介面彙整成一條指令。

設計目標：與 `daemon start` / `daemon kill` 同層級 (同檔案分檔、動詞式子命令)，
複用現有的 `CmdPing` 風格擴充 wire protocol，介面只動 4 個點（`model` / `network.Manager` /
`dispatch` / `Server`），CLI 走「先試連 → 拿到 payload 就格式化、連不上就友善報錯」的雙模式。

## Scope

新增一個 RPC 指令 + 一個 CLI 子命令 + 一個 daemon 內部狀態欄位。
不影響其他 RPC 行為、不動既有檔案的公開介面。

## Wire / Interface Changes

### 1. `model/protocol.go`

新增 `CmdStatus CommandType = "status"`，與其他 `Cmd*` 常數同區段。

### 2. `process/types.go` — 新增型別

```go
// DaemonInfo describes a running PM2 daemon. Returned by CmdStatus
// and rendered by `pm2 daemon status`. Carries the same shape on
// the wire and on disk (future-proofing for save/restore); today
// only the wire path is wired.
type DaemonInfo struct {
    PID          int       `json:"pid"`
    StartedAt    time.Time `json:"started_at"`
    Version      string    `json:"version"`
    HomeDir      string    `json:"home_dir"`
    ProcessCount int       `json:"process_count"`
}
```

放在 `process/`（不是 `model/`）的原因：`daemon/network/handler.go` 已經
`json.Marshal` 過 `process.ProcessInfo`，`process.DaemonInfo` 共用同個
`process` import 即可，不會擴大介面依賴面。

### 3. `daemon/network/manager.go`

在 `Manager` 介面新增一條：

```go
// CmdStatus — daemon identity + light runtime snapshot
Status() process.DaemonInfo
```

並更新介面頂端註解，把 `CmdStatus` 列入對應清單。

### 4. `daemon/network/handler.go`

在 `dispatch` switch 加：

```go
case model.CmdStatus:
    info := m.Status()
    payload, _ := json.Marshal(info)
    return model.Response{OK: true, Payload: payload}
```

與 `CmdList` 同一個 json.Marshal-into-Payload 模式。

### 5. `daemon/server.go`

- `Server` struct 新增 `startedAt time.Time` 欄位。
- `NewServer(homeDir string)` 設 `startedAt: time.Now()` 並把 `PM2Version = "1.0.0"`
  也存成 package-level const（讓 `Status()` 直接讀取，避免魔術字串散落）。
- 新增方法：

  ```go
  // Status returns the daemon's identity + light runtime snapshot.
  // Satisfies network.Manager (CmdStatus).
  func (s *Server) Status() process.DaemonInfo {
      return process.DaemonInfo{
          PID:          os.Getpid(),
          StartedAt:    s.startedAt,
          Version:      PM2Version,
          HomeDir:      s.homeDir,
          ProcessCount: s.reg.Len(),
      }
  }
  ```

  `os` 已在 `server.go` import 清單內；`process.DaemonInfo` 也是新的同 package 型別。

  `ProcessRegistry.Len()` 已存在（`CLAUDE.md` Conventions 有列），無需新增。

## CLI Changes

### 6. `cmd/daemon_status.go` (新檔案)

比照 `cmd/daemon_kill.go` 規模（~50 行）。兩個職責：

- `newDaemonStatusCmd()` — 與 `newDaemonKillCmd` 同樣的 cobra 寫法。
- `runDaemonStatus()` — 雙模式：
  - `model.SendRequest(socketPath(), model.Request{Command: model.CmdStatus})` 成功 →
    `json.Unmarshal(resp.Payload, &info)`、`fmt.Println` 多行格式化輸出。
  - 失敗（dial 錯 / resp.NotOK）→ 印「PM2 daemon is not running.」+ socket 路徑 +
    啟動提示。

輸出格式 (running)：

```text
PM2 daemon
  status:      running
  pid:         12345
  started:     2026-07-05 10:50:23 (5m23s ago)
  uptime:      5m23s
  version:     1.0.0
  home:        /Users/shuk/.pm2
  socket:      /Users/shuk/.pm2/pm2.sock
  processes:   3
```

時間格式用 `2006-01-02 15:04:05`；uptime 用 `time.Since` 算後以
`t.Truncate(time.Second)` 顯示。`shortUptime` 雖然在 `tui/views/format.go`
已經有，但 `cmd` 與 `tui/views` 沒有共用層（tui/views 只 import model/cron 而
不 import cmd），這次在 `cmd/daemon_status.go` 內寫一個本地 4 行格式化即可，
避免為了一個 helper 開新 import。

輸出格式 (not running)：

```text
PM2 daemon is not running.
  socket:      /Users/shuk/.pm2/pm2.sock
  Start with:  pm2 daemon start
```

### 7. `cmd/daemon.go` 註冊子命令

- `newDaemonCmd()` 內 `cmd.AddCommand(newDaemonStatusCmd())`。
- `Long` 描述補上 `status`。
- 父層的 `RunE` fallback 訊息補上 `status`：`"pm2 daemon requires a subcommand (start | kill | status)"`。

## Critical files

- `model/protocol.go` — 加常數
- `process/types.go` — 加型別
- `daemon/network/manager.go` — 介面加方法
- `daemon/network/handler.go` — dispatch 加 case
- `daemon/server.go` — struct 欄位 + Status() 方法 + 版本常數
- `cmd/daemon_status.go` — 新檔
- `cmd/daemon.go` — 註冊 + 文件同步

## Reuse

- `ProcessRegistry.Len()`（`daemon/process_registry.go`） — process count
- `socketPath()`（`cmd/root.go:63`） — CLI 端 socket 路徑
- `pm2Home`（`cmd/root.go:12`） — CLI 端 home dir fallback（給 not-running 分支用）
- `model.SendRequest` / `ReadJSON` / `WriteJSON` — wire helpers
- 既有 `daemon_kill.go` 的「dial 失敗視為 idempotent no-op」精神 — 同樣雙模式

## Verification

1. `go build ./...` — 確認全部 package 編譯通過。
2. `go vet ./...` — 確認無 lint 警告。
3. `go test -race ./...` — 確認 race-free（`Status()` 走 `Len()` 已加讀鎖）。
4. 單元測試（`daemon/network/handler_test.go` 既有風格）：
   - mock `Manager.Status()` 回固定 `DaemonInfo`，送 `CmdStatus`，assert payload 解碼後欄位相等。
5. Smoke test (manual)：
   - 啟動 daemon：`./pm2 daemon start`
   - 跑 `./pm2 daemon status` → 應印 running + 正確 pid / started / process_count
   - `./pm2 daemon kill` 後再跑 → 應印 not running
   - 沒啟動直接跑 `./pm2 daemon status` → 應印 not running + 友善提示
