# 修復 `stdout` 日誌檔案路徑不符合預期之實施計畫 (Fix stdout Log File Path Implementation Plan)

本計畫旨在解決 `pm2 start` 時，若在生態系設定檔 (ecosystem config) 中指定了 `out_file`，但 `stdout` 日誌路徑依然採用預設路徑，而未套用該設定的問題。

## 使用者審查需求 (User Review Required)

> [!NOTE]
> 本次修改在 `AppConfig`、`AppStartReq` 與 `DumpEntry` 中新增了 `out_file` 欄位以支援相容性，不影響現有的 `log_file` 欄位運作。

## 開放性問題 (Open Questions)

無。

## 預定變更 (Proposed Changes)

---

### 配置與協定模組 (Config & Protocol Modules)

#### [MODIFY] [ecosystem.go](file:///Users/shuk/projects/pm2/config/ecosystem.go)
- 在 `AppConfig` 結構體中新增 `OutFile` 欄位，tag 設定為 `json:"out_file"`。
- 在 `AppConfig.Normalize()` 方法中加入邏輯：如果 `LogFile` 為空且 `OutFile` 不為空，則將 `LogFile` 設定為 `OutFile`。

#### [MODIFY] [protocol.go](file:///Users/shuk/projects/pm2/daemon/protocol.go)
- 在 `AppStartReq` 結構體中新增 `OutFile` 欄位，tag 設定為 `json:"out_file"`。

#### [MODIFY] [types.go](file:///Users/shuk/projects/pm2/process/types.go)
- 在 `DumpEntry` 結構體中新增 `OutFile` 欄位，tag 設定為 `json:"out_file"`。

---

### 命令與守護進程模組 (CLI Command & Daemon Modules)

#### [MODIFY] [start.go](file:///Users/shuk/projects/pm2/cmd/start.go)
- 在建構 `daemon.Request` 時，將 `app.OutFile` 賦值給 `daemon.AppStartReq.OutFile`。

#### [MODIFY] [server.go](file:///Users/shuk/projects/pm2/daemon/server.go)
- 在 `launchProcess()` 中，於讀取 `req.LogFile` 之後，若 `logFile` 為空且 `req.OutFile` 不為空，則 fallback 使用 `req.OutFile`。
- 在 `resurrect()` 中，於建構 `AppStartReq` 時，將 `e.OutFile` 賦值給 `req.OutFile`。

---

## 驗證計畫 (Verification Plan)

### 自動化測試 (Automated Tests)
- 於 `config/ecosystem_test.go` (若有) 或新增單元測試，驗證 `Normalize()` 後 `LogFile` 能正確讀取 `OutFile` 的值。

### 手動驗證 (Manual Verification)
1. 建立一個包含 `out_file` 與 `error_file` 的測試設定檔 `test.config.js`：
   ```javascript
   module.exports = {
     apps: [{
       name: "test-watcher",
       script: "sleep",
       args: ["100"],
       out_file: "~/.config/test_watcher/stdout.log",
       error_file: "~/.config/test_watcher/stderr.log"
     }]
   }
   ```
2. 執行 `pm2 start test.config.js`。
3. 執行 `pm2 status` 或 `pm2 monit` 查看 `stdout` 日誌路徑是否正確對齊至 `/Users/shuk/.config/test_watcher/stdout.log`。
4. 檢查實際產生的日誌檔案路徑是否正確。
