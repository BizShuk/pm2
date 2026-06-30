# 修正 Go 編譯錯誤 (Fix Go Build Error)

修正專案在執行 `go build` 時發生的編譯錯誤 (compilation error)。原因為部份原始碼檔案引用了舊的模組名稱 `github.com/shuk/pm2`，而實際在 `go.mod` 中宣告的模組名稱為 `github.com/bizshuk/pm2`。

## 使用者審查請求 (User Review Required)

此修正將會更變以下檔案的匯入路徑 (import path)：
- `cmd/stop.go`
- `daemon/server_test.go`
- `tui/model_test.go`

同時也會將說明文件 (documentation) 中的專案模組名稱修正，以符合實際的設定：
- `CLAUDE.md`
- `README.md`

## 提議的變更 (Proposed Changes)

### 命令列工具元件 (CLI Component)

#### [MODIFY] [stop.go](file:///Users/bytedance/projects/pm2/cmd/stop.go)
更新匯入路徑，將 `github.com/shuk/pm2/daemon` 修正為 `github.com/bizshuk/pm2/daemon`。

---

### 背景服務元件 (Daemon Component)

#### [MODIFY] [server_test.go](file:///Users/bytedance/projects/pm2/daemon/server_test.go)
更新測試檔案中的匯入路徑，將 `github.com/shuk/pm2/process` 修正為 `github.com/bizshuk/pm2/process`。

---

### 文字介面元件 (TUI Component)

#### [MODIFY] [model_test.go](file:///Users/bytedance/projects/pm2/tui/model_test.go)
更新測試檔案中的匯入路徑，將 `github.com/shuk/pm2/process` 修正為 `github.com/bizshuk/pm2/process`。

---

### 說明文件元件 (Documentation Component)

#### [MODIFY] [CLAUDE.md](file:///Users/bytedance/projects/pm2/CLAUDE.md)
將模組名稱由 `github.com/shuk/pm2` 修正為 `github.com/bizshuk/pm2`。

#### [MODIFY] [README.md](file:///Users/bytedance/projects/pm2/README.md)
將儲存庫網址 (repository URL) 中的 `github.com/shuk/pm2` 修正為 `github.com/bizshuk/pm2`。

## 驗證計畫 (Verification Plan)

### 自動化測試 (Automated Tests)
- 於終端機 (terminal) 執行 `go build .` 驗證專案是否能成功編譯。
- 於終端機 (terminal) 執行 `go test ./...` 確保所有測試皆能順利通過。

### 手動驗證 (Manual Verification)
- 無特殊手動驗證需求。
