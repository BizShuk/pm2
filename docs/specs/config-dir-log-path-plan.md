# 支援 `config_dir` 日誌目錄設定之實施計畫 (Support config_dir Log Directory Implementation Plan)

本計畫旨在調整設計，新增 `config_dir` 欄位。若使用者於生態系設定檔中給定此目錄，系統將自動將 stdout 與 stderr 日誌儲存於該目錄下的 `logs/` 資料夾內。

## 使用者審查需求 (User Review Required)

> [!NOTE]
> 當設定 `config_dir: "~/.config/xxxx"` 時：
> - `stdout log` (即 `log_file` 或 `out_file`) 預設對齊至 `~/.config/xxxx/logs/daemon.log`
> - `stderr log` (即 `error_file`) 預設對齊至 `~/.config/xxxx/logs/daemon.err`
>
> 若同時設定了 `out_file` 或 `error_file`，則以個別指定的路徑為優先。

## 開放性問題 (Open Questions)

無。

## 預定變更 (Proposed Changes)

---

### 配置與協定模組 (Config & Protocol Modules)

#### [MODIFY] [ecosystem.go](file:///Users/shuk/projects/pm2/config/ecosystem.go)
- 在 `AppConfig` 結構體中新增 `ConfigDir` 欄位，tag 設定為 `json:"config_dir"`。
- 在 `AppConfig.Normalize()` 中，若 `ConfigDir` 不為空：
  - 當 `LogFile` 為空且 `OutFile` 為空時，將 `LogFile` 設定為 `filepath.Join(ConfigDir, "logs", "daemon.log")`
  - 當 `ErrorFile` 為空時，將 `ErrorFile` 設定為 `filepath.Join(ConfigDir, "logs", "daemon.err")`

#### [MODIFY] [protocol.go](file:///Users/shuk/projects/pm2/daemon/protocol.go)
- 在 `AppStartReq` 結構體中新增 `ConfigDir` 欄位，tag 設定為 `json:"config_dir"`。

#### [MODIFY] [types.go](file:///Users/shuk/projects/pm2/process/types.go)
- 在 `DumpEntry` 結構體中新增 `ConfigDir` 欄位，tag 設定為 `json:"config_dir"`。

---

### 命令與守護進程模組 (CLI Command & Daemon Modules)

#### [MODIFY] [start.go](file:///Users/shuk/projects/pm2/cmd/start.go)
- 在建構 `daemon.Request` 時，將 `app.ConfigDir` 傳遞給 `daemon.AppStartReq.ConfigDir`。

#### [MODIFY] [server.go](file:///Users/shuk/projects/pm2/daemon/server.go)
- 在 `launchProcess()` 中：
  - 若 `logFile` 為空，且 `req.ConfigDir` 不為空，則 fallback 設定為 `filepath.Join(req.ConfigDir, "logs", "daemon.log")`
  - 若 `errFile` 為空，且 `req.ConfigDir` 不為空，則 fallback 設定為 `filepath.Join(req.ConfigDir, "logs", "daemon.err")`
- 在 `save()` 中，將 `ConfigDir` 儲存至 `DumpEntry`（若有）。
- 在 `resurrect()` 中，將 `e.ConfigDir` 傳遞給 `req.ConfigDir`。

---

## 驗證計畫 (Verification Plan)

### 自動化測試 (Automated Tests)
- 於 `config/ecosystem_test.go` 中新增或執行測試，確認當 `ConfigDir` 有值而 `LogFile` 與 `ErrorFile` 為空時，`Normalize()` 能正確套用路徑。

### 手動驗證 (Manual Verification)
1. 修改 `/Users/shuk/projects/inf/ecosystem.config.js`：
   ```javascript
   {
       namespace: "Infra",
       name: "File Watcher",
       script: "file_watcher",
       args: ["monitor"],
       config_dir: "~/.config/file_watcher",
       autorestart: true
   }
   ```
2. 執行 `pm2 start ecosystem.config.js`。
3. 執行 `pm2 save` 後，檢查 `~/.pm2/dump.json` 中 `File Watcher` 的 `log_file` 與 `error_file` 是否分別被對齊至：
   - `/Users/shuk/.config/file_watcher/logs/daemon.log`
   - `/Users/shuk/.config/file_watcher/logs/daemon.err`
