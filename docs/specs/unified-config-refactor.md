# Unified Process Configuration 重構計畫

## Context

pm2 專案目前有 4 個結構體重複定義行程配置的相同欄位：`config.AppConfig`（18 個欄位 + `Normalize()`）、`model.AppStartReq`（19 個欄位）、`process.DumpEntry`（17 個欄位）、`process.ProcessInfo`（22 個欄位）。資料在這 4 個類型間流轉時必須手動逐欄位映射（`cmd/start.go` 建構 AppStartReq、`daemon/persistence.go` 的 save/resurrect 內 ProcessInfo↔DumpEntry↔AppStartReq 來回拷貝、`daemon/server.go::watchProcess` 與 `restartByName` 內部重啟時再從 mp.Info 拷貝回 AppStartReq），且 `daemon/persistence.go:37` 還有 `OutFile: mp.Info.LogFile` 的明顯 bug（把 LogFile 誤拷貝到 OutFile）。

**目標**：以 `process.AppConfig` 為唯一真相來源，透過 Go anonymous embedding 把 `AppStartReq` 與 `ProcessInfo` 改為「內嵌 + 差量欄位」，最後刪除 `DumpEntry` 並把 `daemon/persistence.go` 改為直接處理 `[]AppConfig`。

**預期效益**：
- 新增欄位（如健康檢查、CPU 親和性）只需改 `process.AppConfig` 一處
- 4 處手動映射代碼（cmd/start.go、daemon/persistence.go save、daemon/persistence.go resurrect、daemon/server.go watchProcess/restartByName 共 5 處）全部消失
- 自然修復 `daemon/persistence.go:37` 的 OutFile bug
- `resurrect()` 內呼叫 `AppConfig.Normalize()` 補上預設值，確保從 dump 恢復的行程與手動啟動行為一致

**已確認決策**：
1. `BaseEnv` 統一放入 `process.AppConfig`（連同 JSON tag `base_env,omitempty`），符合既有 `ProcessInfo`/`DumpEntry`/`AppStartReq` 三處的行為；dump.json 直接持久化
2. dump.json **不處理向後相容** — 新版 daemon 對舊 dump 直接報錯，使用者需手動 `pm2 delete all && pm2 save` 後重新設定
3. `ProcessInfo.LogFile` / `ProcessInfo.ErrorFile` **完全刪除** — 統一使用 `AppConfig.LogFile` / `AppConfig.ErrorFile`（launchProcess 內解析後回填到 AppConfig）

---

## 整體策略

採用「**先建立單一真相、再向上 embed、向下消除**」的 4 階段漸進式重構。每個 Phase 之間保留 `go build ./...` 與相關套件測試全綠的可回滾點；建議每個 Phase 一個獨立 commit，方便 `git revert HEAD~N` 單獨回滾。

| Phase | 範圍 | 影響檔案數 | 風險 |
|---|---|---|---|
| 1 | 建立 process.AppConfig + Normalize 遷移 | ~8 檔 | 低（純欄位搬移） |
| 2 | model.AppStartReq 內嵌 AppConfig | ~4 檔 | 低（Protocol JSON 形狀不變） |
| 3 | process.ProcessInfo 內嵌 AppConfig | ~10 檔 | 中（刪除重複欄位 + promoted field） |
| 4 | 刪除 DumpEntry + 統一持久化 | ~3 檔 | 中（dump.json 格式變更 + 向後不相容） |

---

## Phase 1：建立 process.AppConfig（Complexity: Medium）

### 目標
- 在 `process/types.go` 新增 `AppConfig` 結構，**欄位與 JSON tag 必須與現有 `config/ecosystem.go:15-33` 完全相同**，並新增 `BaseEnv` 欄位（沿用 `model/protocol.go:69` 的 tag `json:"base_env,omitempty"`）
- 將 `Normalize()` 從 `config/ecosystem.go:41-85` 完整遷移到 `process/types.go` 作為 `AppConfig` 的 method
- `config/ecosystem.go` 改為定義 `EcosystemConfig` 但 `Apps` 改為 `[]process.AppConfig`；移除 `AppConfig` struct 與其 `Normalize()`
- 5 個 cmd 檔案（`eco.go`/`eco_renderer.go`/`eco_wizard.go`/`eco_install.go`/`eco_test.go`）內 `config.AppConfig` → `process.AppConfig`

### 檔案改動

| 檔案 | 改動 |
|---|---|
| `process/types.go` | 新增 `AppConfig` struct + `BaseEnv` 欄位（tag `json:"base_env,omitempty"`）+ `Normalize()` 方法 |
| `config/ecosystem.go` | 刪除 `AppConfig` struct（保留 `EcosystemConfig`，但 `Apps` 改為 `[]process.AppConfig`）；刪除 `Normalize()` 方法；`Load/loadJSON/loadJS` 內呼叫改為 `process.AppConfig{...}.Normalize()`；保留 `resolveScriptPath` helper |
| `cmd/eco.go`、`cmd/eco_renderer.go`、`cmd/eco_wizard.go`、`cmd/eco_install.go` | 所有 `config.AppConfig` → `process.AppConfig`；struct literal 一併改寫 |
| `config/ecosystem_test.go` | 不需改（無類型引用） |
| `cmd/eco_test.go` | 全檔 `config.AppConfig` → `process.AppConfig`；append 至 `process.AppConfig{...}` |

### `process.AppConfig` 精確定義

完整複製 `config/ecosystem.go:15-33` 的 18 個欄位與 JSON tag，再追加 `BaseEnv`：

```go
type AppConfig struct {
    Namespace   string            `json:"namespace"`
    Name        string            `json:"name"`
    Script      string            `json:"script"`
    Args        []string          `json:"args"`
    Env         map[string]string `json:"env"`
    CronRestart string            `json:"cron_restart"`
    Cron        string            `json:"cron"`
    Watch       bool              `json:"watch"`
    MaxRestarts int               `json:"max_restarts"`
    Version     string            `json:"version"`
    LogFile     string            `json:"log_file"`
    OutFile     string            `json:"out_file"`
    ErrorFile   string            `json:"error_file"`
    ConfigDir   string            `json:"config_dir"`
    ConfigFile  string            `json:"config_file"`
    CWD         string            `json:"cwd"`
    Instances   int               `json:"instances"`
    BaseEnv     []string          `json:"base_env,omitempty"` // CLI environment snapshot, persisted for resurrect
}
```

> 重要：JSON tag 必須逐字對齊 `config/ecosystem.go`；`omitempty` 規則也照搬。`Instances` 在原 `config/ecosystem.go` 沒有 tag `omitempty`（必填），維持原樣。

### 引用點替換模式

```go
// 改動前
var app config.AppConfig
app := config.AppConfig{Name: "api", Script: "main.js"}

// 改動後
var app process.AppConfig
app := process.AppConfig{Name: "api", Script: "main.js"}
```

`config.EcosystemConfig.Apps` 從 `[]config.AppConfig` 改為 `[]process.AppConfig` 後，`cfg.Apps[i].Name` 等欄位存取語法不變。

### 驗證

```bash
go build ./...
go test -v ./config/...
go test -v ./cmd/...
```

---

## Phase 2：model.AppStartReq 內嵌 AppConfig（Complexity: Low）

### 目標
- `model.AppStartReq` 改為 anonymous embed `process.AppConfig`，僅保留 `CronTriggered` 欄位（`BaseEnv` 已隨嵌入進入 AppConfig，移除獨立欄位）
- JSON 序列化形狀保持向下相容（所有原欄位 tag 仍會被扁平化為 JSON 物件頂層）
- `cmd/start.go` 與 `daemon/server.go` 內 3 處建構 `AppStartReq` 的程式碼簡化

### 檔案改動

| 檔案 | 改動 |
|---|---|
| `model/protocol.go` | `AppStartReq` 內刪除 18 個與 `AppConfig` 重複的欄位與獨立 `BaseEnv`；改為 embed `process.AppConfig` + 保留 `CronTriggered` |
| `cmd/start.go` | 建構 `AppStartReq` 改為 `&model.AppStartReq{AppConfig: process.AppConfig{...}, CronTriggered: ..., BaseEnv: os.Environ()}` |
| `daemon/server.go` | `watchProcess` 與 `restartByName` 內 2 處 `&model.AppStartReq{...}` 構造點改寫 |
| `model/protocol_test.go` | `TestAppStartReqRoundTrip` 對 `"cron_restart":"@every 1h"` 等子字串斷言應仍通過（驗證時若失敗，改用 map 比對） |

### `model.AppStartReq` 精確定義

```go
type AppStartReq struct {
    process.AppConfig
    CronTriggered bool `json:"cron_triggered"`
}
```

### 建構語法簡化範例

```go
// 改動前（cmd/start.go）
req := &model.AppStartReq{
    Namespace: app.Namespace, Name: app.Name, Script: app.Script,
    Args: app.Args, Env: app.Env, CronRestart: app.CronRestart,
    Cron: app.Cron, Instances: app.Instances, MaxRestarts: app.MaxRestarts,
    Version: app.Version, LogFile: app.LogFile, OutFile: app.OutFile,
    ErrorFile: app.ErrorFile, ConfigDir: app.ConfigDir, Watch: app.Watch,
    ConfigFile: app.ConfigFile, CWD: app.CWD,
    BaseEnv: os.Environ(),
}

// 改動後
req := &model.AppStartReq{
    AppConfig:     app, // value copy
    CronTriggered: false,
}
// 將 CLI 環境快照注入到嵌入的 AppConfig
req.AppConfig.BaseEnv = os.Environ()
```

`daemon/server.go` 內部重啟（`watchProcess` / `restartByName`）的建構改為：

```go
ac := mp.Info.AppConfig                              // value copy
ac.Env = maps.Clone(mp.Info.AppConfig.Env)           // map 隔離
ac.BaseEnv = append([]string(nil), mp.Info.AppConfig.BaseEnv...) // slice 隔離
req := &model.AppStartReq{AppConfig: ac}
```

> 注意：`Env` map 為引用型別，淺拷貝會共享；用 `maps.Clone`（Go 1.21+ 標準庫）做隔離。`BaseEnv` slice 改用 `append([]string(nil), ...)` 隔離。

### JSON 序列化相容性確認

- 序列化形狀：所有 `AppConfig` 欄位的 JSON tag 會被「提升」到 `AppStartReq` 頂層；外加 `cron_triggered` — 與舊版完全相同
- `model/protocol_test.go::TestAppStartReqRoundTrip` 對 `"cron_restart":"@every 1h"` 等子字串斷言：內嵌後欄位名稱不變，應直接綠燈

### 驗證

```bash
go test -v ./model/...
go test -v ./cmd/...
go build ./...
```

---

## Phase 3：process.ProcessInfo 內嵌 AppConfig（Complexity: High）

### 目標
- `process.ProcessInfo` 改為 anonymous embed `process.AppConfig`，僅保留 9 個 runtime 欄位
- 刪除 `ProcessInfo` 內重複的 16 個配置欄位（包含 `LogFile` / `ErrorFile`，語意統一）
- `daemon/server.go::launchProcess` 內建構 `ProcessInfo` 的程式碼簡化
- tui 內 ~50 處 `p.Script` / `p.LogFile` 等讀取透過 promoted field 維持不變

### 檔案改動

| 檔案 | 改動 |
|---|---|
| `process/types.go` | `ProcessInfo` 內刪除所有與 `AppConfig` 重複的欄位（含 `LogFile` / `ErrorFile`）；改為 embed `AppConfig` + 9 個 runtime 欄位 |
| `daemon/server.go` | `launchProcess` 內建構 `ProcessInfo` 改用 `AppConfig: process.AppConfig{...}` 形式；解析後的 log/error 絕對路徑回填到 `info.AppConfig.LogFile` / `info.AppConfig.ErrorFile` |
| `daemon/manager.go` | 既有 `mp.Info.Name` / `mp.Info.Namespace` 等讀取透過 promoted field 維持不變 |
| `daemon/metrics.go` | `mp.Info.PID` / `mp.Info.Status`（runtime 欄位）維持不變 |
| `daemon/watcher.go` | `req.Name` / `req.Script` 等透過 AppStartReq 嵌入的 AppConfig promoted field 維持不變 |
| `tui/model.go`、`tui/renderer.go`、`tui/formatter.go` | `p.Script` / `p.LogFile` / `p.Cron` 等 ~50 處透過 promoted field **不需改** |
| `tui/model_test.go` | 建構 `process.ProcessInfo{...}` 約 10 處改為 `process.ProcessInfo{AppConfig: process.AppConfig{...}, ID: ..., PID: ...}` |

### `process.ProcessInfo` 精確定義

```go
type ProcessInfo struct {
    process.AppConfig

    // 9 個 runtime 欄位
    ID             int       `json:"id"`
    PID            int       `json:"pid"`
    Status         Status    `json:"status"`
    Restarts       int       `json:"restarts"`
    StartedAt      time.Time `json:"started_at"`
    CPU            float64   `json:"cpu"`
    Memory         uint64    `json:"memory"`
    User           string    `json:"user"`
    LastCronAt     time.Time `json:"last_cron_at"`
    LastCronStatus string    `json:"last_cron_status"`
}
```

### launchProcess 建構簡化

`daemon/server.go::launchProcess` 內現有約 25 個欄位的 `process.ProcessInfo{...}` 構造改為：

```go
// 改動前
mp := &ManagedProcess{
    Info: process.ProcessInfo{
        ID: id, Namespace: ns, Name: name, PID: pid, Status: status,
        StartedAt: startedAt, Script: req.Script, Args: req.Args,
        Env: req.Env, CronRestart: req.CronRestart, Cron: req.Cron,
        LastCronAt: lastCronAt, LastCronStatus: lastCronStatus,
        LogFile: logFile, ErrorFile: errFile, MaxRestarts: req.MaxRestarts,
        ConfigDir: req.ConfigDir, Version: version, User: currentUser,
        Watch: req.Watch, ConfigFile: req.ConfigFile, Restarts: restarts,
        CWD: req.CWD, BaseEnv: req.BaseEnv,
    },
}

// 改動後
mp := &ManagedProcess{
    Info: process.ProcessInfo{
        AppConfig: process.AppConfig{
            Namespace: ns, Name: name, Script: req.Script,
            Args: req.Args, Env: req.Env, CronRestart: req.CronRestart,
            Cron: req.Cron, MaxRestarts: req.MaxRestarts,
            ConfigDir: req.ConfigDir, Version: version, Watch: req.Watch,
            ConfigFile: req.ConfigFile, CWD: req.CWD, BaseEnv: req.BaseEnv,
            // LogFile / ErrorFile 由 launchProcess 解析後回填到下面
            LogFile: logFile, ErrorFile: errFile,
        },
        ID: id, PID: pid, Status: status,
        StartedAt: startedAt, Restarts: restarts,
        User: currentUser, LastCronAt: lastCronAt, LastCronStatus: lastCronStatus,
    },
}
```

> log/error 絕對路徑解析（`launchProcess` 第 269-296 行的 `homedir.Expand` 等邏輯）結果直接寫入 `info.AppConfig.LogFile` / `info.AppConfig.ErrorFile`，與舊版 `info.LogFile` 等價（promoted field 讀取仍可工作）。

### 內部重啟的 AppStartReq 深拷貝

`watchProcess` 與 `restartByName` 兩處（`daemon/server.go:546`、`daemon/server.go:648`）的 `&model.AppStartReq{...}` 構造點改為：

```go
ac := mp.Info.AppConfig
ac.Env = maps.Clone(mp.Info.AppConfig.Env)
ac.BaseEnv = append([]string(nil), mp.Info.AppConfig.BaseEnv...)
req := &model.AppStartReq{AppConfig: ac}
```

### 驗證

```bash
go test -v ./daemon/...
go test -v ./tui/...
go build ./...
```

---

## Phase 4：刪除 DumpEntry，統一持久化（Complexity: Medium）

### 目標
- 完全刪除 `process.DumpEntry`（從 `process/types.go` 移除）
- `daemon/persistence.go::save()` 直接序列化 `[]process.AppConfig`
- `daemon/persistence.go::resurrect()` 反序列化後呼叫 `Normalize()`，再丟給 `s.startApp`
- **不處理 dump.json 向後相容**（已確認決策）：resurrect() 對舊 `[]DumpEntry` 格式直接 log 錯誤並回傳失敗
- 更新 `daemon/server_test.go` 內 3 處 `[]process.DumpEntry` 引用

### 檔案改動

| 檔案 | 改動 |
|---|---|
| `process/types.go` | 刪除 `DumpEntry` struct |
| `daemon/persistence.go` | `save()` 序列化 `[]process.AppConfig`；`resurrect()` 嘗試解析 `[]process.AppConfig`，失敗時回傳明確錯誤 |
| `daemon/server_test.go` | `TestWatchStateInheritance` / `TestVersionStateInheritance` / `TestSaveConcurrentWithMapMutation` 內 `var entries []process.DumpEntry` 改為 `var entries []process.AppConfig`；建構處同步改寫；斷言 `entries[0].Watch` 維持不變（`Watch` 是 `AppConfig` 欄位） |

### save() 改寫

```go
func (s *Server) save() error {
    s.mu.RLock()
    apps := make([]process.AppConfig, 0, len(s.processes))
    for _, mp := range s.processes {
        apps = append(apps, mp.Info.AppConfig)
    }
    s.mu.RUnlock()

    dumpPath := filepath.Join(s.homeDir, "dump.json")
    data, err := json.MarshalIndent(apps, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(dumpPath, data, 0o644)
}
```

> 同時修復 `daemon/persistence.go:37` 的 bug：原 `OutFile: mp.Info.LogFile` 改為直接從 `mp.Info.AppConfig.OutFile` 取值（結構內嵌後每個欄位都從正確位置取，bug 自然消失）。

### resurrect() 改寫

```go
func (s *Server) resurrect() error {
    dumpPath := filepath.Join(s.homeDir, "dump.json")
    data, err := os.ReadFile(dumpPath)
    if err != nil {
        return fmt.Errorf("no dump found (run pm2 save first): %w", err)
    }

    var apps []process.AppConfig
    if err := json.Unmarshal(data, &apps); err != nil {
        return fmt.Errorf("dump.json format incompatible (Phase 4 unified-config refactor — please run `pm2 delete all` then re-add your apps, or restore from a pre-refactor backup): %w", err)
    }

    for i := range apps {
        apps[i].Normalize() // 確保從 dump 恢復的行程與手動啟動行為一致
        req := &model.AppStartReq{AppConfig: apps[i]}
        if _, err := s.startApp(req); err != nil {
            slog.Info("resurrect error", "name", apps[i].Name, "err", err)
        }
    }
    return nil
}
```

### 對應測試更新

`daemon/server_test.go` 三處改寫：

```go
// 改動前
var entries []process.DumpEntry
entries = append(entries, process.DumpEntry{Watch: true, ...})
if entries[0].Watch != true { ... }

// 改動後
var entries []process.AppConfig
entries = append(entries, process.AppConfig{Watch: true, ...})
if entries[0].Watch != true { ... }  // Watch 欄位名稱相同，語法不變
```

### 驗證

```bash
go test -race -v ./daemon/...
go build -o /dev/null ./...
```

---

## 風險評估與緩解

| # | 風險 | 影響 | 緩解策略 |
|---|---|---|---|
| 1 | **JSON 序列化扁平化順序變動**：`AppStartReq` 內嵌 `AppConfig` 後，`json.Marshal` 輸出會把嵌入欄位排在父欄位之後。本專案無外部 SDK 強依賴欄位順序，CLI 與 TUI 都用 Go 解析 | 無 | `model/protocol_test.go::TestAppStartReqRoundTrip` 為 substring 包含斷言，不依賴順序，應綠燈 |
| 2 | **dump.json 向後不相容**：既有 `~/.pm2/dump.json` 為 `[]DumpEntry` 格式，新 daemon resurrect() 會失敗 | 使用者需手動 delete + 重新設定 | 已在錯誤訊息中說明原因與處理步驟；Phase 4 驗證用 `go test -race` 涵蓋 |
| 3 | **log_file/error_file 雙重定義**：Phase 3 刪除 `ProcessInfo.LogFile/ErrorFile`，若有任何程式碼依賴「啟動配置原值」與「runtime 解析後值」分離 | 邊界場景下 log 路徑錯誤 | 確認 `launchProcess` 內解析後回填到 `info.AppConfig.LogFile`；`tui/*.go` 內 `p.LogFile` 透過 promoted field 仍可讀取 |
| 4 | **tui 的 promoted field 行為**：~50 處 `p.Script` / `p.LogFile` 等讀取在 Phase 3 後透過 promoted field 自動解析為 `p.AppConfig.Script` | 極少數欄位讀到錯誤值（若父子層有遮蔽） | Phase 3 刪除 `ProcessInfo` 內重複欄位時務必清空；`tui/model_test.go` 內既有測試覆蓋大部分欄位 |
| 5 | **Env map 共享**：Phase 2 / 3 內 value copy `process.AppConfig` 會共享 `Env` map 與 `BaseEnv` slice | 重啟時可能汙染原 `mp.Info` 狀態 | 內部重啟處（watchProcess / restartByName）一律用 `maps.Clone` + `append([]string(nil), ...)` 隔離 |

---

## 關鍵檔案

- `/Users/bytedance/projects/tmp/pm2/process/types.go`（核心：4 個類型統一）
- `/Users/bytedance/projects/tmp/pm2/config/ecosystem.go`（Phase 1：移除 AppConfig）
- `/Users/bytedance/projects/tmp/pm2/model/protocol.go`（Phase 2：AppStartReq 內嵌）
- `/Users/bytedance/projects/tmp/pm2/daemon/persistence.go`（Phase 4：統一持久化）
- `/Users/bytedance/projects/tmp/pm2/daemon/server.go`（Phase 2/3：建構點簡化 + 內部重啟）
- `/Users/bytedance/projects/tmp/pm2/cmd/start.go`（Phase 2：建構點簡化）
- `/Users/bytedance/projects/tmp/pm2/cmd/eco_*.go`（Phase 1：引用點替換）
- `/Users/bytedance/projects/tmp/pm2/daemon/server_test.go`（Phase 4：測試更新）
- `/Users/bytedance/projects/tmp/pm2/cmd/eco_test.go`（Phase 1：測試更新）
- `/Users/bytedance/projects/tmp/pm2/tui/model_test.go`（Phase 3：測試更新）

---

## 驗證策略

### 階段驗證（每個 Phase 完成後）
```bash
# Phase 1
go build ./... && go test -v ./config/... && go test -v ./cmd/...

# Phase 2
go test -v ./model/... && go test -v ./cmd/... && go build ./...

# Phase 3
go test -v ./daemon/... && go test -v ./tui/... && go build ./...

# Phase 4
go test -race -v ./daemon/... && go build -o /dev/null ./...
```

### 最終全量驗證
```bash
go build ./...
go test -race -count=3 ./...    # race detector 跑 3 次確保穩定性
go vet ./...
gofmt -l .                      # 確保沒有未格式化的檔案
```

### dump.json 格式驗證（手動）
```bash
# 啟動一個 app，pm2 save，cat dump.json 確認為新格式
./pm2 start
./pm2 save
cat ~/.pm2/dump.json | jq '.[0] | keys' | grep -E '(instances|base_env|out_file)'
```

### 提交策略
4 個獨立 commit：
```
refactor(process): Phase 1 - introduce process.AppConfig as single source of truth
refactor(model): Phase 2 - embed process.AppConfig into AppStartReq
refactor(process): Phase 3 - embed process.AppConfig into ProcessInfo
refactor(daemon): Phase 4 - remove DumpEntry, unify persistence on []AppConfig
```

每個 commit 之間 `go build ./...` 與 `go test ./...` 必須全綠，方便 `git revert HEAD~N` 單獨回滾。
