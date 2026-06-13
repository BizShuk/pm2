# task

- `[x]` 實作非同步主機指標收集 (Asynchronous Host Metrics Collection)
    - `[x]` 修改 `tui/model.go` 定義新訊息並於 `Model` 引入指標快取欄位
    - `[x]` 實作非同步背景命令 `updateHostMetricsCmd` 並在 `Init()` 中發送
    - `[x]` 於 `Update()` 中處理 `hostMetricsMsg` 與 `triggerHostMetricsMsg` 訊息，更新快取並定時重置背景命令
    - `[x]` 修改 `buildHostMetricsStr` 成員方法，移除同步讀取並改用快取指標
    - `[x]` 修改 `buildListTUI` 中的呼叫方式
- `[x]` 驗證修改與測試
    - `[x]` 執行 `go test ./...` 驗證程式碼正常運作
    - `[x]` 執行 `go run . m` 進行手動效能與功能驗證
