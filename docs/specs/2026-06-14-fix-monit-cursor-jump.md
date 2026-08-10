# 實作計畫：修復 `pm2 monit` 重新整理時游標隨機跳動之問題 (Fix Cursor Jumping Issue on Refresh)

本計畫旨在修復在 `pm2 monit` 介面（即 `pm2 m`）中，當列表重新整理時，游標會隨機跳動至其他進程的問題。此現象在游標指向未啟動的 cron 進程時尤為明顯。

## 使用者審查項目 (User Review Required)

- `修復核心`: 在覆寫進程列表 `m.procs = msg.procs` 之前，先提取並記錄當前選取進程的唯一識別碼 `ID`，以確保重新整理與排序後，游標仍能正確鎖定在同一個進程上。

## 開放性問題 (Open Questions)

- 無。此問題屬於單純的狀態同步錯誤，修復邏輯相當明確。

---

## 預定變更內容 (Proposed Changes)

### TUI 模組 (TUI Module)

#### [MODIFY] [model.go](file:///Users/shuk/projects/tmp/pm2/tui/model.go)

- `修改 sortProcs 簽章`:
  將 `sortProcs` 修改為接受一個可選的舊選取 ID `prevSelectedID int`。若傳入 `-1` 則會從當前的 `m.procs[m.selected]` 中自動獲取。

- `修正 refreshMsg 處理邏輯`:
  在 `refreshMsg` 的 Case 區段中：
    1. 在覆寫 `m.procs` 之前，先將當前選中進程的 `ID` 存入暫存變數。
    2. 將新資料賦值給 `m.procs`。
    3. 呼叫 `m.sortProcs(selectedID)`，傳入暫存的 `selectedID` 進行排序並恢復游標位置。

- `更新其他 sortProcs 呼叫處`:
  在 `handleKey` 處理 `s` 鍵（切換排序）時，呼叫 `m.sortProcs(-1)`。

---

## 驗證計畫 (Verification Plan)

### 自動化測試 (Automated Tests)

- 在 [model_test.go](file:///Users/shuk/projects/tmp/pm2/tui/model_test.go) 中新增測試 `TestRefreshPreservesSelection`：
    - 模擬初始進程列表，並設定選中其中一個進程。
    - 觸發 `refreshMsg`，傳入一個順序不同（例如 ID 順序）的最新進程列表。
    - 驗證更新並排序後，`m.selected` 所指針的進程 `ID` 仍與原先選中的進程相同，且不會發生跳動。
- 執行自動化測試：`go test ./tui/...`

### 手動驗證 (Manual Verification)

- 啟動多個進程，其中包含一個 cron 進程（例如 `pm2 start test.js --cron "*/5 * * * *"`，其未執行時狀態為 stopped）。
- 執行 `pm2 m -d` 開啟監控面板。
- 將游標移動到該 cron 進程（或任何進程）上。
- 觀察每 2 秒一次的自動重新整理（Tick），確認選取的游標一直穩定停留在該進程，不再隨機跳移到其他進程。
