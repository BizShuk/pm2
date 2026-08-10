# 在監控列表中實時檢查進程 CPU 與記憶體使用率之實施計畫 (Check Process CPU & Mem Usage in Monitor List Implementation Plan)

本計畫旨在解決 `pm2 monit` 子命令（不帶 `-d` 的 `runLiveList` 介面）中，進程的 `cpu` 和 `mem` 欄位可能顯示為預設值或不即時的問題。我們將在 CLI 的 `monitor` 模組中新增一個實時查詢進程系統資源的函數，並在每秒更新畫面時直接調用以取得最新數據。

## 使用者審查需求 (User Review Required)

> [!NOTE]
> 本次變更僅影響 `pm2 monit` 表格模式的畫面渲染，不涉及 Daemon 後端 RPC 協議變更，且無破壞性變動。

## 開放性問題 (Open Questions)

無。

## 預定變更 (Proposed Changes)

---

### 監控命令模組 (Monitor Command Module)

#### [MODIFY] [monitor.go](file:///Users/shuk/projects/tmp/pm2/cmd/monitor.go)

- 新增 `getProcessMetrics(pid int) (float64, uint64)` 函數：
    - 當傳入的 `pid <= 0` 時，直接返回 `0, 0`。
    - 使用 `exec.Command("ps", "-p", fmt.Sprintf("%d", pid), "-o", "%cpu,rss")` 執行查詢。
    - 解析 `ps` 指令的輸出（第二行），讀取 `%CPU` 與 `RSS`。
    - 將 `RSS` 乘上 `1024` 以將單位從 `KB` 換算為 `Byte`，然後返回 `(cpu, mem)`。
- 修改 `runLiveList()` 函數：
    - 在 `json.Unmarshal(resp.Payload, &infos)` 解析出進程列表後，遍歷所有進程。
    - 若進程 status 為 `process.StatusOnline` 且 `PID > 0`，則呼叫 `getProcessMetrics(p.PID)`，將取得的即時 CPU 與記憶體數值更新至 `infos` 對應項目的 `CPU` 與 `Memory` 欄位中。

---

## 驗證計畫 (Verification Plan)

### 自動化測試 (Automated Tests)

- 執行 `go test ./...` 確保現有的進程控制與通訊協議測試皆能通過。

### 手動驗證 (Manual Verification)

1. 確保 daemon 已啟動，且有正在執行的進程（例如先前 resurrected 的 `Port Listenor`、`File Watcher` 等）。
2. 執行 `go run . m` 開啟監控列表。
3. 觀察 `Port Listenor` 與 `File Watcher` 的 `cpu` 與 `mem` 欄位。
4. 檢查 `mem` 欄位是否正確顯示為 `10.4mb` 或 `19.8mb` 等非零值，且隨著時間刷新。
5. 檢查 `cpu` 欄位是否偶爾顯示微幅的變動（例如 `0.1%` 或 `0.2%`），而非永久固定在 `0.0%`。
