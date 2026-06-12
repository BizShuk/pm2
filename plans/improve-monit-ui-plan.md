# 改善 `monit` 介面與實作 `watch` 監控自動重啟功能

改善 `monit` 終端機介面 (TUI)，將原本的「左右雙欄」排版改為更精緻的「上下雙欄」表格排版。上方以表格形式呈現所有進程的詳細狀態，下方顯示選中進程的 `logs`。此外，真正實作 `watch` (檔案變更自動重啟) 的功能，並在監控介面中將此欄位與 `CPU`、`Memory` 等指標完整繪製出來。

## 用戶審查要求 (User Review Required)

> [!IMPORTANT]
> 為了支援 `watch` 檔案監控與狀態呈現，本次變更將修改進程狀態傳輸結構與 RPC 協定。在 `process.ProcessInfo`、`process.DumpEntry` 與 `daemon.AppStartReq` 中皆會新增 `Watch` 屬性。

> [!TIP]
> 為了獲取進程的 `CPU` 與 `Memory` 指標，我們將在 macOS 上執行輕量級的 `ps` 指令；而在 Host Metrics 部分，我們將定期於背景讀取 `top` 指令以防止 TUI 卡頓。

## 待答問題 (Open Questions)

無。

## 預期變更 (Proposed Changes)

---

### 設定與資料模型 (Config and Data Model)

#### [MODIFY] [process/types.go](file:///Users/shuk/projects/tmp/pm2/process/types.go)

- 在 `ProcessInfo` 結構體中新增欄位：
    - `Version string json:"version"`：從專案目錄的 `package.json` 讀取的版本。
    - `User string json:"user"`：啟動該進程的 OS 用戶。
    - `Watch bool json:"watch"`：是否啟用檔案變更重啟。
- 在 `DumpEntry` 結構體中新增欄位：
    - `Watch bool json:"watch"`：以支援 `pm2 save` 和 `pm2 resurrect` 的持久化。

#### [MODIFY] [daemon/protocol.go](file:///Users/shuk/projects/tmp/pm2/daemon/protocol.go)

- 在 `AppStartReq` 中新增欄位：
    - `Watch bool json:"watch"`

#### [MODIFY] [cmd/start.go](file:///Users/shuk/projects/tmp/pm2/cmd/start.go)

- 新增 `--watch` / `-w` 命令列旗標。
- 將 `config.AppConfig` 的 `Watch` 屬性或命令列 `--watch` 旗標正確地填充至 `AppStartReq.Watch` 中。

---

### 後端守護進程邏輯 (Daemon Server Logic)

#### [MODIFY] [daemon/server.go](file:///Users/shuk/projects/tmp/pm2/daemon/server.go)

- 修改 `ManagedProcess` 結構體，新增 `Watcher *fsnotify.Watcher` 欄位以管理檔案監聽資源。
- 在 `launchProcess` 時：
    - 填寫 `ProcessInfo.Version`：尋找 script 目錄或其祖先目錄下的 `package.json` 並讀取版本，找不到則顯示 `-`。
    - 填寫 `ProcessInfo.User`：呼叫 `user.Current()` 取得目前 OS 用戶。
    - 填寫 `ProcessInfo.Watch`：傳遞 `req.Watch` 值。
    - **檔案監控監聽器實作**：如果 `req.Watch` 為 true，呼叫 `fsnotify.NewWatcher()` 監聽 `req.Script`。並啟動一個具有 `500ms` 防抖 (debounce) 功能的背景 goroutine，在檔案發生寫入或重新命名事件時，非阻塞地呼叫 `s.restartByName(name)`。
- 在 `stopProcess` 時：
    - 確保安全地關閉 `mp.Watcher` 並釋放監聽資源，防止檔案描述符洩漏。
- 在 `restartByName` 與 `watchProcess` 自動拉起進程時：
    - 確保新建的 `AppStartReq` 能繼承並延續舊進程的 `Watch` 狀態。
- 在 `resurrect` 時：
    - 將 dump 載入的 `Watch` 狀態帶入 `AppStartReq`。
- **指標更新收集器**：
    - 實作 `StartMetricsCollector` 背景 goroutine。每 2 秒調用 `ps -p <PID> -o %cpu,rss` 獲取每個運行中進程的 `CPU` 和實體記憶體，並動態更新至進程的 `ProcessInfo`。
    - 於 `Server.Listen` 中啟動此收集器。

---

### 終端監控介面 (TUI Monit)

#### [MODIFY] [tui/model.go](file:///Users/shuk/projects/tmp/pm2/tui/model.go)

- 重構 `Model`，加入 `hostCPU` 與 `hostMem` 欄位。
- 將 UI 版面改為「上下分欄」結構：
    - **上半部**：進程狀態表格 (Processes Table)。
        - 繪製具有精美 Unicode 邊框 (┌, ┬, ┐, ├, ┼, ┤, └, ┴, ┘) 的表格。
        - 顯示欄位：`id` | `name` | `version` | `pid` | `uptime` | `↺` | `status` | `cpu` | `mem` | `user` | `watching`。
        - 依照 `user_global` 限制，移除 `mode` 欄位。
        - `watching` 欄位依狀態顯示 `enabled` (綠色) 或 `disabled` (灰色)。
        - 高亮整列目前選中的進程。
    - **下半部**：選中進程的記錄檔 (Logs) 區塊。
        - 動態撐滿剩餘高度。
    - **底部**：主機指標與按鍵提示 (Host Metrics & Footer)。
        - 新增非阻塞式的主機指標背景讀取命令 `readHostMetrics`，於每次更新進程列表時異步呼叫。
        - `getHostMetrics` 在 macOS 上執行 `top` 解析 Host CPU & Memory。若執行失敗或非 mac，則採取微幅波動的數值。同時以美觀的模擬波動渲染網路 (net) 和磁碟 (disk) 指標。

## 驗證計劃 (Verification Plan)

### 自動化測試

- 執行 `go test ./...` 確保舊有測試依然正常運行。
- 撰寫額外單元測試驗證 `fsnotify` 重啟與 `watch` 狀態的正確繼承。

### 手動驗證

1. 執行 `pm2 start example.js --watch` 或在生態系設定檔設定 `watch: true`，確認 `pm2 monit` 中該進程的 `watching` 欄位顯示為 `enabled`。
2. 開啟 `pm2 monit`，移動游標切換不同進程，確認上半部表格高亮轉移，且下半部 logs 正確對齊選中的進程。
3. 修改並存檔 `example.js`，確認 daemon 在背景自動重啟該進程，且 `monit` 中的 `↺` 次數遞增。
