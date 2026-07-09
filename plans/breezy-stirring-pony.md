# 程式碼品質改善計畫 — pm2

## Context

針對 `/Users/bytedance/projects/tmp/pm2` 的靜態品質審查（不合理資料夾結構、一致性、single file single responsibility）發現了以下問題：

- 兩個核心檔案超過 500 行且混合多個職責（`daemon/process_manager.go` 581 行、`tui/model.go` 515 行）
- 死碼符號（`DumpEntry`、`CmdLogs`、`Request.Follow`）
- 版本號 hard-code 兩處、錯誤訊息/格式重複、process 查找邏輯重複
- 目錄結構碎片（`docs/superpowers/` 孤立目錄、root-level scratch 檔案）

本計畫分四個獨立階段逐步修正。

---

## Phase 1 — 死碼移除

### 1.1 移除 `process.DumpEntry`

- `process/types.go`: 刪除 lines 152-178（`DumpEntry` struct + 上方 comment block）
- `CLAUDE.md:266`: `[]process.DumpEntry` → `[]process.AppConfig`
- `README.todo`: 移除 lines 70, 73, 113 中的 `DumpEntry` 相關文字

### 1.2 移除 `model.CmdLogs` + `Request.Follow`

- `model/protocol.go:33`: 刪除 `CmdLogs CommandType = "logs"`
- `model/protocol.go:47`: 刪除 `Follow bool` 欄位
- `model/protocol_test.go`: 刪除 `CmdLogs`/`Follow` 測試案例（lines 39-43），修正 round-trip 斷言（line 59）

### 驗證

```bash
go build ./... && go vet ./... && go test ./...
grep -rn "DumpEntry\|CmdLogs" --include="*.go" .  # 零結果
grep -n "Follow" model/*.go                        # 零結果
```

---

## Phase 2 — 一致性修正

### 2.1 版本號集中至 `model` 套件

- `model/protocol.go`: 在 const block 中加入 `const PM2Version = "1.0.0"`
- `daemon/server.go`: 刪除 `const PM2Version = "1.0.0"`（lines 15-19）
- `daemon/process_manager.go:310`: `Version: PM2Version,` → `Version: model.PM2Version,`
- `cmd/root.go`: `fmt.Println("1.0.0")` → `fmt.Println(model.PM2Version)`，新增 `model` import

### 2.2 錯誤訊息去重複

- `daemon/process_manager.go`: 新增 `errors` import，定義：
  ```go
  var errProcessNotFound = errors.New("process or namespace not found")
  func processNotFoundError(name string) error {
      return fmt.Errorf("%w: %s", errProcessNotFound, name)
  }
  ```
- 替換 5 處 `fmt.Errorf("process or namespace not found: %s", name)` → `processNotFoundError(name)`
  （lines 129, 143, 176, 199, 229）

### 2.3 cron key helper

- 新建 `daemon/cron_key.go`：
  ```go
  package daemon

  func cronKey(ns, name string) string { return ns + ":" + name }
  ```
- 替換 `daemon/process_manager.go` 中所有 `ns + ":" + name` 內聯建構（~10 處）
- 替換 `daemon/process_registry.go:300` 中 `ns + ":" + name`

### 驗證

```bash
go build ./... && go test -race ./daemon/...
go build -o /tmp/pm2v2 . && /tmp/pm2v2 version  # 輸出 1.0.0
```

---

## Phase 3 — 檔案拆分（Single Responsibility）

### 3.1 拆分 `daemon/process_manager.go`（581 行 → 3 檔）

所有檔案保持在 `package daemon`，無 import 變更。

| 檔案 | 內容 |
|------|------|
| `process_manager.go`（保留，修剪） | `ManagedProcess`、`ProcessManager` struct、`NewProcessManager`、lock delegates、12 個 RPC 方法、`findProcesses`、`StartMetricsCollector`、`refreshMetrics` |
| `launch.go`（新建） | `launchProcess`（~135 行） |
| `lifecycle.go`（新建） | `onProcessExit`、`stopProcess`、`triggerCron` |

### 3.2 拆分 `tui/model.go`（515 行 → 3 檔）

所有檔案保持在 `package tui`。

| 檔案 | 內容 |
|------|------|
| `model.go`（保留，修剪） | const blocks、message types、`Model` struct、`New`、`Init`、`Update`、`applyRefresh`、`recomputeNamespaces`、`applyNamespaceFilter`、`cycleNamespace`、`sortProcs`、`cycleSort`、`View` |
| `keys.go`（新建） | `handleKey`、`pauseOrResume` |
| `commands.go`（新建） | `doRefresh`、`readLogs`、`doAction` |

### 驗證

```bash
go build ./... && go vet ./...
go test -race ./daemon/... && go test -race ./tui/...
wc -l daemon/*.go tui/*.go  # 無單檔超過 ~450 行
```

---

## Phase 4 — 目錄結構清理

### 4.1 刪除孤立目錄

```bash
rm -rf docs/superpowers/
```

（內容已被 `docs/specs/` 覆蓋）

### 4.2 清理 root-level 雜物

```bash
rm ecosystem.config.js  # scratch config，含 hard-coded 路徑
rm run.sh               # 一行 ln -s，開發用 scratch
```

### 4.3 延後項目

`config/remote.go` 提升為 `config/remote/` 子套件：涉及較大的重構（`Wizard`、`Env`、cobra 註冊都要移動），另開計畫處理。

### 驗證

```bash
go build ./... && go test ./...
ls docs/superpowers 2>&1        # No such file or directory
ls ecosystem.config.js run.sh   # 皆 No such file or directory
git status                      # 僅預期的刪除與編輯
```

---

## 最終驗證

```bash
go build ./... && go vet ./... && go test ./...
go test -race -count=2 ./...
go build -o /tmp/pm2bin .

# Smoke test
rm -rf ~/.pm2
/tmp/pm2bin daemon start
sleep 1
/tmp/pm2bin start --name smoke --script /bin/sh -- -c "while true; do echo hi; sleep 1; done"
/tmp/pm2bin list           # 應顯示 smoke 在 online 狀態
/tmp/pm2bin stop smoke
/tmp/pm2bin daemon kill    # daemon 乾淨退出
/tmp/pm2bin version        # 輸出 1.0.0（證明 CLI 與 daemon 版本已連結）
```

---

## 階段依賴關係

- Phase 1 → 2 → 3 → 4，依序執行
- Phase 1 最先：移除 `DumpEntry` 後 doc 檔案才不會有衝突
- Phase 2 的行號基準在 Phase 1 之後；Phase 3 的行號基準在 Phase 2 之後（行號漂移很小，單行數字級別）
- Phase 4 獨立於所有 code phase，放在最後以便看到完整圖像再刪除非 Go 檔案
