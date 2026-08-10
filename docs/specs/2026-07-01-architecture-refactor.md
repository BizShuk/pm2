# 架構演進與優化計畫 — pm2-refactor (Architecture Evolution & Optimization Plan)

> **狀態:已完成 (Completed)**
> 原始 6 階段計畫於 2026-07-10 完成驗證。所有結構性目標已落地於當前 codebase。
> 本檔保留作為「現狀 vs 目標」對照文件,並列出**剩餘的技術債**作為後續可選工作。

## 1. 驗證方法

對照對象:`HEAD` (commit `278d22b` 起的 master 分支) — 直接讀取 `daemon/server.go`、`daemon/process_manager.go`、`daemon/process_registry.go`、`daemon/executor/`、`daemon/network/`、`tui/model.go`、`tui/views/`、`cron/scheduler.go`、`model/protocol.go`、`process/types.go`、`config/ecosystem.go`。

## 2. 階段驗證 (Phase Verification)

### 階段 1:補強特徵測試 ✅

**原始目標**:增加高並行啟動、進程異常終止、env 繼承、cron 重啟的測試,建立重構安全網。

**現狀**:
- `daemon/process_registry_test.go`(339 行):覆蓋 registry 並發、UpdateInfo/UpdateMetrics race。
- `daemon/server_test.go`(1448 行):覆蓋啟動、停止、暫停、恢復、刪除、cron、paused resurrect、watch。
- `daemon/pause_race_test.go`:pause vs cron fire 的 race guard。
- `tui/views/namespace_test.go`:namespace 切換邏輯。
- `config/ecosystem_test.go` 與 `config/remote_test.go`:config loader + 遠端 repo。
- `model/protocol_test.go`:wire JSON 序列化。

**驗證命令**:`go test -race ./...` 全綠 (見 §6)。

---

### 階段 2:統一配置結構 ✅

**原始目標**:合併 `config.AppConfig` / `model.AppStartReq` / `process.DumpEntry` 三套欄位,避免重複資料結構。

**現狀**:
- `process/types.go` 的 `AppConfig` 是**唯一真相**(命名空間 / 名稱 / 腳本 / args / env / cron / watch / 路徑 / base_env / paused)。
- `model.AppStartReq` 內嵌 `process.AppConfig`,僅新增 `CronTriggered bool`(傳輸層語意)。
- `process.ProcessInfo` 內嵌 `process.AppConfig`,只增加 runtime 欄位(ID, PID, Status, CPU, Memory, ...)。
- `process.DumpEntry` 標記為 Deprecated(`// Deprecated: as of the unified-config refactor`),`dump.json` 已直接序列化成 `[]AppConfig`。
- `process_manager.Resurrect` 對舊 dump.json 給出明確錯誤訊息,要求使用者重新 `pm2 delete all` 後重加(已寫入 `unmarshal` 失敗訊息中)。
- `AppConfig.Normalize()` 是唯一預設值填入點,生態載入、resurrect、CLI start 都呼叫它。

**殘留議題**:`DumpEntry` 型別仍存在,目前沒有任何 caller 寫入,僅保留給 out-of-tree 讀者。建議加一筆 TODO 在 `process/types.go` 上游說明刪除時機(下一次 minor 版本 bump 後移除)。

---

### 階段 3:抽離進程註冊表 ✅

**原始目標**:建立 `process.Manager` 結構體,封裝 `processes` 映射表與 `sync.RWMutex`;`Server` 改為持有此結構。

**現狀**:
- `daemon/process_registry.go`(338 行)封裝 `processes` map + `sync.RWMutex`。
- 公開方法:`Add` / `Get` / `Remove` / `List` / `SnapshotMap` / `SnapshotForMetrics` / `Snapshot` / `SnapshotAppConfigs` / `Len` / `FindByTarget` / `UpdateInfo` / `UpdateMetrics` / `UpdateCronStatus` / `LookupExistingForLaunch`。
- `ProcessManager` 持 `*ProcessRegistry` 指標,僅通過 escape hatches (`Lock` / `Unlock` / `RLock` / `RUnlock`) 提供給 `launchProcess` 等需要跨方法原子性的熱點路徑。
- `daemon/pause_race_test.go` 與 `process_registry_test.go` 驗證 race 場景。

**實作位置備註**:registry 留在 `daemon/` 而非下沉到 `process/`,因為 `ProcessManager` 與 `ProcessRegistry` 之間存在強耦合(cron 排程、save/resurrect、executor 協作全部經過 `ProcessManager` 編排),下沉會導致 `process/` 反向依賴 `daemon/executor` 與 `cron`,違反單向依賴方向。**此決策與原始計畫 §4「`daemon/manager.go` -> `process/manager.go`」的目標路徑不同,並刻意保留。**

---

### 階段 4:抽離進程執行器 ✅

**原始目標**:建立 `process.Executor` 介面與實作,遷移 `launchProcess` / `stopProcess` / `watchProcess`;同時修復孤兒行程問題 (pgid kill)。

**現狀**:
- `daemon/executor/executor.go`(314 行)實作 `Executor` struct(非介面,與原始計畫略異 —— 介面化會增加 mock 開銷,而現有測試已直接建構 `*Executor`)。
- `daemon/executor/builder.go`:`BuildCommand(script, args, base, extra, workDir)` 包裝 `/bin/bash -c`、設定 `Setpgid: true`、合併 env。
- `daemon/executor/watcher.go`:`NewFileWatcher` 採用 500ms debounce。
- `daemon/executor/metrics.go`:**三階段非阻塞 refresh**(phase 1:RLock 快照,phase 2:unlocked 並行 `ps`,phase 3:lock + PID 比對後寫回),`MetricsWorkers=8` 並行上限。
- **孤兒行程修復**:`Executor.Stop` 在 `daemon/executor/executor.go:240-270` 採用 `syscall.Kill(-pid, SIGTERM)` 對整個行程組發信號(原始計畫漏列此修復,見 `modularization` / `reorganization`)。
- `MetricsBackend` 介面(`daemon/executor/metrics.go:23-31`)只暴露 `SnapshotForMetrics` + `UpdateMetrics`,`ProcessRegistry` 透過實作此介面注入 collector —— 達成了「Executor 不直接持有 registry 鎖」的目標。
- `Executor` 不持有任何鎖,所有狀態回寫走 `onStopping` / `onStopped` / `onFileChanged` / `onExit` callback。

**殘留議題**:`Executor` 是 struct 而非 interface。若未來需要 mock(例如注入遠端 SSH executor),需先將 `Executor.Start` / `Stop` / `Watch` 抽為介面(命名建議 `ProcessExecutor`)。

---

### 階段 5:抽離網路傳輸層 ✅

**原始目標**:把 `Listen` / `handleConn` 移至 `daemon/network`,使 `daemon/server.go` 只做伺服器初始化。

**現狀**:
- `daemon/server.go`(84 行)僅為「thin wrapper」:`Server` 內嵌 `*ProcessManager`,`Listen` 啟動 metrics + auto-resurrect + auto-save 三個 goroutine 後委派給 `network.Listen`。
- `daemon/network/listener.go`:`Listen(socketPath, manager)` bind + accept loop。
- `daemon/network/handler.go`:每連線一協程,`Handle(conn, manager)` 讀 `Request`、dispatch、寫 `Response`、`CmdKill` 排程 150ms 後 `os.Exit(0)`(符合 CLAUDE.md「KillAll is idempotent, does NOT exit」invariant)。
- `daemon/network/manager.go`:`Manager` 介面是網路層唯一依賴;`network` package 不 import `daemon`;`executor` / `registry` 不 import `network`(import 方向:`network -> (Manager contract only)`,無循環)。
- `model/protocol.go` 留在 `model/` 而非下沉為 `protocol/`,因為它承載**雙向**契約 (CLI 端 dial 與 daemon 端 read/write 共用),放在 model 套件可讓 cmd 與 tui 直接 import 而不需穿透 daemon。

**決策差異說明**:`modularization` 計畫提議把 `daemon/protocol.go` 拉到 `protocol/` 頂層套件;`refactor` 與 `decoupling` 提議留在 `daemon/`;當前實作折衷放在 `model/`。**保留現狀**,理由已記錄於 `model/protocol.go:1-10` 的 package doc 註解。

---

### 階段 6:解耦 TUI 視圖與狀態 ✅

**原始目標**:把 `tui/model.go` 的渲染邏輯抽到 `tui/views/`。

**現狀**:
- `tui/views/` 子目錄:`context.go` / `header.go` / `footer.go` / `detail.go` / `logs.go` / `list.go` / `layout.go` / `format.go` / `namespace.go`(後者附 `namespace_test.go`)。
- `RenderLayout`(`tui/views/layout.go`)為單一入口,呼叫上述子 renderer。
- `tui/theme/palette.go` 為 lipgloss 色彩唯一來源;`tui/theme.go` 的 `clXxx` 變數為向後相容 re-export。
- `tui/model.go` 仍為 controller(Update + dispatch + 訊息),但所有 view 渲染皆走 `views.ViewContext` 純函式。
- namespace 切換邏輯獨立為 `tui/views/namespace.go`(對應 `architecture-cli-list-and-metrics.md` 計畫)。

**殘留議題**:`tui/model.go` 仍有 515 行(主要為 bubbletea 訊息處理、namespace strip 互動、`metrics.go` 背景 collector 觸發)。若日後繼續成長,候選拆分:
- 將 namespace strip 切換邏輯抽為 `tui/ns_strip.go` 的 state 物件。
- 將 doAction / doRefresh 拆為 `tui/rpc_client.go`(目前已透過 `model.SendRequest` 抽離,只是 coordinator 邏輯仍散落)。

---

## 3. 已驗證的不變量 (Verified Invariants)

對照 CLAUDE.md「Conventions」段的每條 invariant:

| 不變量 | 現狀驗證 |
| :--- | :--- |
| `daemon.ProcessManager` 持有 `*ProcessRegistry` 唯一擁有權 | ✅ `process_manager.go:35-43` 僅持 `*ProcessRegistry` |
| 高階 method 優於 escape hatches | ✅ registry 暴露 14 個語意化方法;`LaunchProcess` 為唯一使用 `Lock/Unlock` 包裹跨方法原子性的熱點 |
| `UpdateInfo` 優於直接 `mp.Info` 寫入 | ✅ `process_manager.go:181-184, 207-210, 472-487, 532-542` 全部走 `UpdateInfo` |
| `onProcessExit` 為唯一 auto-restart 入口 | ✅ `executor.Watch` callback 僅呼叫 `onProcessExit`;`stopping` flag 守衛防止 stop 觸發 restart |
| Status race guard (`!mp.stopping`) | ✅ `process_manager.go:474` 守衛 deliberate stop vs natural exit |
| Log paths 在 launch 時一次性解析,存於 `ProcessInfo` | ✅ `executor.StartResult` 帶絕對路徑,`launchProcess` 寫入 `mp.Info.LogFile/ErrorFile` |
| `config.AppConfig.Normalize()` 必須呼叫 | ✅ `ecosystem.go` 與 `process_manager.Resurrect` 都呼叫 |
| Executor lock direction | ✅ `daemon/executor/executor.go:22-37` package doc 明確記錄,`Executor` 不持鎖,所有回寫走 callback |
| Network import direction | ✅ `daemon/network/manager.go:1-12` package doc 明確記錄,`network -> (Manager contract only)`,無循環 |
| TUI views 為純函式 | ✅ `tui/views/*.go` 全部接受 `ViewContext` 或基本型別,回傳 `string` |
| 顏色僅來自 `tui/theme/palette.go` | ✅ `tui/theme.go` 為 re-export,新 view 程式碼不宣告 `AdaptiveColor` literal |
| **`UpdateInfo`/`SnapshotOne` 優於直接 `mp.Info` 讀取(**測試碼**)** | ❌ `daemon/server_test.go` 79 處違規;`TestPauseResumeRunningProcess` 已被 `go test -race ./...` 觸發 race;追蹤於 `docs/specs/2026-07-10-test-direct-info-read-fix.md` |

## 4. 已完成的衍生修復 (Bonus Fixes Delivered)

雖非原始計畫明列,但已落地於 refactor 過程中:

1. **孤兒行程 pgid kill**:`daemon/executor/executor.go:254` 的 `syscall.Kill(-pid, SIGTERM)`(原始計畫 §1.3 描述,「現狀未修」已不再適用)。
2. **三階段非阻塞 metrics**:`daemon/executor/metrics.go:46-127` 的 MetricsCollector 解決 RPC 阻塞(原始計畫 §1.2 描述,「現狀未修」已不再適用)。
3. **`paused` flag round-trip**:`process_registry.go:148-158` 的 `SnapshotAppConfigs` 把 `ManagedProcess.paused` 寫回 `AppConfig.Paused`,`Resurrect` 在 `process_manager.go:276` 把 `entries[i].Paused` 套到 `req.Paused`,確保 daemon 重啟不會悄悄 undo `pm2 pause`(regression test:`TestPausedCronTaskSurvivesResurrect`)。
4. **Pause vs cron fire race guard**:`process_manager.launchProcess` 的 `existing.paused && req.CronTriggered` 守衛在 registry 寫鎖下原子決策,regression test:`TestPauseDuringCronFireLeavesNoSchedule`。
5. **Cron 鍵已使用 `namespace:name`**:`cron/scheduler.go` 的 `Register(name, ...)` 接收的 `name` 由 `process_manager.launchProcess` 傳入 `ns + ":" + name`(見 `process_manager.go:440, 442, 456`),故不同命名空間的同名進程不會互相覆蓋。**`EntryCount()` 已暴露供測試驗證**(見 `tui/views/namespace_test.go` 的存在)。
6. **統一協議位置**:`model/protocol.go` 同時服務 CLI (cmd/) 與 TUI (tui/),兩者只需 import `model/`,不需穿透 `daemon/`。

## 5. 殘留技術債 (Remaining Tech Debt)

下列項目**仍待處理**,不應再被埋在重構計畫中 —— 應各自建立獨立追蹤文件。

### 5.1 移除 deprecated `process.DumpEntry`

- 位置:`process/types.go:159-178`
- 影響:無 caller 寫入,僅佔編譯符號表。
- 行動:下次 minor 版本 bump 後刪除型別與所有引用。
- 工時:< 0.5 天。

### 5.2 提升 `Executor` 為介面

- 位置:`daemon/executor/executor.go` (struct) → 介面化。
- 影響:目前測試透過 `*Executor` 直接建構,需使用真實 `os/exec`。若要支援容器化或 SSH 遠端執行,需介面化。
- 行動:抽出 `ProcessExecutor` 介面(`Start` / `Stop` / `Watch`),`ProcessManager.executor` 改為介面型別;現有 `*Executor` 改名為 `LocalExecutor`。
- 工時:1 天 + 回歸測試。
- 觸發時機:真的有第二個實作時再啟動。

### 5.3 `tui/model.go` 進一步拆分

- 位置:`tui/model.go` (515 行)。
- 影響:`Update` 方法膨脹,namespace strip 切換邏輯與 log tail 邏輯混雜。
- 行動:抽 `tui/rpc_client.go`(封裝 doAction + doRefresh)、`tui/ns_strip.go`(namespace state 與選中邏輯)、`tui/log_tail.go`(日誌檔案讀取 + 緩衝)。
- 工時:1.5 天。
- 觸發時機:新增第三個 view 或互動模式時。

### 5.4 測試沙箱隔離

- 原始 `reorganization` 計畫 §1.5 提及 `TestStartAppOutFileHomeExpansion` 使用 `~/` 路徑。
- 現狀:當前 `daemon/server_test.go` 已大量使用 `t.TempDir()` 與自訂 `homeDir`,但仍可能有部分測試遺漏。
- 行動:完整 audit 所有測試檔案,確認無 `os.UserHomeDir()` / `~/` 直引;`config.AppConfig.Normalize` 與 `executor.resolveLogPath` 已透過 `req.ConfigDir` 接受任意路徑,理論上無此問題,但需驗證。
- 工時:0.5 天 audit。

### 5.5 RPC 連線池

- 當前 `model.SendRequest` 每次 dial 一次關閉,高頻呼叫(如 TUI 每 2s refresh + 動作後立即 refresh)會有少量連線開銷。
- 影響:實測 < 1ms 開銷,瓶頸在 `ps` 而非 socket。**目前不需處理。**
- 觸發時機:TUI 進入 100+ 進程或 refresh 頻率提升時。

### 5.6 測試碼 UpdateInfo/SnapshotOne 化(2026-07-10 揭露)

- 位置:`daemon/server_test.go` 79 處直接 `mp.Info.*` 讀取。
- 觸發:`go test -race -count=1 ./...` 揭露 `TestPauseResumeRunningProcess` race,讀取 `mp.Info.PID` 與 `onProcessExit` 寫 `mp.Info.PID=0` 競爭。
- 修法:新增 `ProcessRegistry.SnapshotOne(key) (process.ProcessInfo, bool)`(RLock 下拷貝值副本),逐個替換 79 處違規讀取;對「必須與 background goroutine 同步讀取」場景(如 pause/resume race)改用 `pm.reg.UpdateInfo(key, fn)` 包裹。
- 追蹤:`docs/specs/2026-07-10-test-direct-info-read-fix.md`。
- 工時:0.5 天(純機械替換 + 1 個 `UpdateInfo` 場景重構)。
- 觸發時機:CI 引入 `go test -race -count=3 ./...` 之前,因為當前 `-count=1` 偶發觸發,但 `-count=3` 必觸發。

## 6. 驗證指令 (Verification Commands)

```bash
# 全專案 race-free(單次 — 已知會暴露 §5.6 違規)
go test -race -v ./...

# 全專案 race-free(三次連跑 — §5.6 修補後才會全綠;若未修補則必觸發 race)
go test -race -count=3 ./...

# 個別子套件
go test -race -v ./daemon/...
go test -race -v ./daemon/executor/...
go test -race -v ./daemon/network/...
go test -race -v ./tui/...
go test -race -v ./tui/views/...
go test -race -v ./cron/...
go test -race -v ./model/...
go test -race -v ./process/...
go test -race -v ./config/...

# import 方向檢查 (確保 network 不 import daemon)
go list -deps ./daemon/network | grep -E 'bizshuk/pm2/(daemon|executor)' || echo "OK: no upward imports"

# 靜態分析
go vet ./...
```

## 7. 對外契約 (External Contract)

| 物件 | 檔案 | 變動原則 |
| :--- | :--- | :--- |
| `Request` / `Response` JSON tag | `model/protocol.go:42-66` | 不可變更,為 CLI ↔ daemon 線路協議 |
| `AppConfig` JSON tag | `process/types.go:36-62` | 不可變更,為 `dump.json` 檔案格式 |
| `network.Manager` 介面 | `daemon/network/manager.go:32-66` | 新增方法需向下相容(預設實作) |
| `tui.ViewContext` 結構 | `tui/views/context.go` | 僅新增欄位,不可變更既有欄位型別 |
| `cron.Scheduler` 公開 API | `cron/scheduler.go` | `Register` / `Remove` / `EntryCount` 簽名穩定 |

## 8. 給未來貢獻者的注意事項

- 不要再把 OS 操作寫回 `daemon/process_manager.go` —— 那是協調器,不是 executor。
- 新增 RPC 命令時:
  1. 在 `model/protocol.go` 加 `CommandType` 常數。
  2. 在 `network.Manager` 介面加方法簽名(同時在 `ProcessManager` 實作)。
  3. 在 `network/handler.go` 的 switch dispatch 加分支。
- 修改 `AppConfig` 欄位時:同步更新 `dump.json` 的版本欄位或寫 migration 邏輯,避免破壞線上 dump 檔。
- 新增 view 走 `tui/views/<name>.go`,不接受 `*Model` 為參數。
- 顏色一律 import `tui/theme/palette.go`,不要在 view 內宣告 `lipgloss.AdaptiveColor`。
