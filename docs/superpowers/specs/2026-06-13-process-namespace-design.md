# 進程命名空間設計 (Process Namespace Design)

本文件描述在 `pm2` 進程管理器中引入 `命名空間 (namespace)` 的設計與實作方式。

## 設計目標

在每個受管理的 `進程 (process)` 中引入 `命名空間 (namespace)` 的概念，支援多個進程的邏輯分組。

- 預設命名空間：若啟動時未指定 `namespace`，預設為 `default`。
- 操作優先級：在進行進程管理操作時，指定的目標參數將依據 `進程 ID (process ID)` > `進程名稱 (process name)` > `命名空間 (namespace)` 的順序進行命中與操作。
- 批次操作：若命中 `namespace`，將對該命名空間下的所有進程執行操作。
- 同名支援：允許不同命名空間下存在相同名稱的進程。

---

## 異動範圍

### 1. 資料結構與協定 (Data Structures & Protocol)

我們需要在各組態與狀態結構中新增 `namespace` 欄位。

- `config.AppConfig`
  新增 `Namespace string` 欄位。在 `Normalize()` 函數中，若其為空字串，則預設為 `default`。
- `process.ProcessInfo` 與 `process.DumpEntry`
  新增 `Namespace string` 欄位。
- `daemon.AppStartReq`
  新增 `Namespace string` 欄位。

### 2. 儲存唯一鍵值變更 (Daemon State Map Key)

目前 `Server.processes` 使用 `Name` 作為鍵值 (key)。為支援不同命名空間下的同名進程，我們將鍵值格式改為 `Namespace:Name`。
例如，`default:api`。

### 3. CLI 啟動與列表顯示 (CLI Start & List)

- `pm2 start` 指令
  新增 `--namespace` (簡寫 `-ns`) flag。若為 ecosystem 啟動，則由 JS/JSON 配置讀取 `namespace`。
- `pm2 list` 指令
  在表格中新增 `Namespace` 欄位，排在 `Name` 之前。

### 4. 進程匹配與操作邏輯 (Process Matching & Operations)

在 `daemon/server.go` 中實作 `findProcesses(target)` 函數：

1. 若為 `all`，返回所有進程。
2. 嘗試將 `target` 轉為整數匹配 `process ID`。若匹配成功，返回該進程。
3. 嘗試匹配 `process name`。若有匹配，返回所有匹配的進程列表（可能有多個，例如不同命名空間的同名進程，或是多個實例）。
4. 嘗試匹配 `namespace`。若有匹配，返回該命名空間下的所有進程。

`stopByName`、`restartByName`、`deleteByName` 函數皆改用 `findProcesses` 獲取進程列表，並逐一執行對應動作。

### 5. 進程日誌與 TUI 面板 (Logs & TUI)

- `pm2 logs <target>`
  同樣依據 `ID` > `Name` > `Namespace` 順序篩選進程，並印出其對應的日誌檔案。
- `pm2 monit` (Bubbletea TUI)
    - 在 Detail 面板中新增 `namespace` 資訊。
    - 在 TUI 進行重啟、停止、刪除等鍵盤操作時，將發送的 `daemon.Request` 的 `Name` 欄位改為該進程的 `ID` 字串，以保證操作的精確性。

---

## 驗證計畫

### 1. 單元測試與手動功能驗證

- 驗證使用 `--namespace` 啟動進程是否正確歸類。
- 驗證使用 `ecosystem.config.js` 配置 `namespace` 啟動進程。
- 驗證 `pm2 list` 表格與 `pm2 monit` 是否正確顯示命名空間。
- 驗證 `pm2 stop <id>`、`pm2 stop <name>`、`pm2 stop <namespace>` 與 `pm2 stop all` 是否符合優先級命中邏輯。
- 驗證同名進程在不同命名空間中能同時運行且不受干涉。
