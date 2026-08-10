# 日誌路徑向後相容設計之實施計畫 (Log Path Backward Compatible Design Implementation Plan)

本計畫旨在實作向後相容邏輯：若生態系設定檔中未設定 `config_dir` 但有設定 `out_file`、`log_file` 或 `error_file` 時，系統會自動推導並將其父目錄作為 `config_dir` 的值，以相容舊有的設定檔。

## 使用者審查需求 (User Review Required)

> [!NOTE]
> 自動推導邏輯優先順序：
> 1. 若 `config_dir` 已存在，則不進行推導。
> 2. 若 `out_file` 有值，則 `config_dir` 設為 `out_file` 所在的資料夾路徑。
> 3. 若 `out_file` 無值但 `log_file` 有值，則 `config_dir` 設為 `log_file` 所在的資料夾路徑。
> 4. 若上述皆無值但 `error_file` 有值，則 `config_dir` 設為 `error_file` 所在的資料夾路徑。

## 開放性問題 (Open Questions)

無。

## 預定變更 (Proposed Changes)

---

### 配置模組 (Config Module)

#### [MODIFY] [ecosystem.go](file:///Users/shuk/projects/pm2/config/ecosystem.go)
- 在 `AppConfig.Normalize()` 中新增向後相容的 `config_dir` 推導邏輯，程式碼範例如下：
  ```go
  if a.ConfigDir == "" {
      if a.OutFile != "" {
          a.ConfigDir = filepath.Dir(a.OutFile)
      } else if a.LogFile != "" {
          a.ConfigDir = filepath.Dir(a.LogFile)
      } else if a.ErrorFile != "" {
          a.ConfigDir = filepath.Dir(a.ErrorFile)
      }
  }
  ```

---

## 驗證計畫 (Verification Plan)

### 自動化測試 (Automated Tests)
- 於 `config/ecosystem_test.go` 中新增或執行測試，驗證無 `config_dir` 但有 `out_file` 或 `error_file` 時，`config_dir` 的推導是否正確。

### 手動驗證 (Manual Verification)
1. 修改 `/Users/shuk/projects/inf/ecosystem.config.js`，將 `File Watcher` 回復為舊格式，且不包含 `config_dir`：
   ```javascript
   {
       namespace: "Infra",
       name: "File Watcher",
       script: "file_watcher",
       args: ["monitor"],
       out_file: "~/.config/file_watcher/daemon.out",
       error_file: "~/.config/file_watcher/daemon.err",
       autorestart: true
   }
   ```
2. 執行 `pm2 start ecosystem.config.js`。
3. 執行 `pm2 save` 後，檢查 `~/.pm2/dump.json` 中的 `File Watcher` 設定，確認是否自動推導出 `"config_dir": "~/.config/file_watcher"`。
