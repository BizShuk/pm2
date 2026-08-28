# pm2 — 術語表 (Terminology)

本檔是領域名詞、狀態值與縮寫的單一定義來源。CLI、daemon protocol 與文件使用同一組正名。

## 行程模型 (Process Model)

| 術語 (Term) | 英文 (English) | 定義 (Definition) | 出處 (Source) |
| --- | --- | --- | --- |
| 應用 | App | 一份被管理的行程定義，來自 ecosystem config 或 `pm2 start` 參數 | `ecosystem.config.json` |
| 任務 | Task | 已註冊到 daemon 的應用實例；`pm2 task` 命名空間操作的對象 | `process_registry.go` |
| 行程管理器 | Process Manager | daemon 內持有所有任務狀態的核心元件 | `process_manager.go` |
| 登錄表 | Registry | 任務狀態的唯一持有者；查詢與更新都經過它 | `process_registry.go` |
| 執行器 | Executor | 實際 fork+exec、監看、停止與收集指標的元件 | `executor/` |
| 排程器 | Scheduler | 依 cron 運算式觸發任務的元件 | `cron/scheduler.go`、`robfig/cron` |
| 命名空間 | Namespace | 命令的分組前綴，例如 `task` / `daemon` / `logs`；別名保留其子命令 | `pm2 t`、`pm2 d` |

## 編排模型 (Orchestration Model)

| 術語 (Term) | 英文 (English) | 定義 (Definition) | 出處 (Source) |
| --- | --- | --- | --- |
| 工作流 | Workflow | 一組`依序`執行的階段；前一段成功才跑下一段 | `ecosystem.config.js` 的 `workflows:` |
| 階段 | Stage | workflow 的一步；`script` / `task` / `workflow` 三選一 | `workflow/config.go` |
| 分類 | Category | workflow 的分組標籤，等同 app 的 `namespace` | `Config.Category` |
| 執行 | Run | 一次 workflow 執行；以 `20260828T030012-a1b2c3` 形式的 run ID 標識 | `runhistory/runid.go` |
| 觸發來源 | Trigger | 這次執行為何開始：`manual` / `cron` / `webhook` / `nested` / `watch` / `autorestart` / `resurrect` / `restart` / `cron_restart` | `runhistory/record.go` |
| 單飛 | Single Flight | 同一個 workflow 同時只允許一次 run；`環防護的真正防線` | `wfengine.ErrRunInProgress` |
| 引擎 | Workflow Engine | 持有定義、排程與進行中 run 的元件 | `daemon/wfengine` |
| 掛鉤 | Webhook | 觸發 workflow 的 HTTP 端點；`對外開放且無認證` | `POST /api/webhooks/<name>` |

> `一次性 (one-shot)`：stage `只執行一次`，成功的唯一定義是 exit code 為 0。
> auto-restart、`cron_restart`、`watch`、`instances` 一律`不適用`於 stage
> ——它們描述的是「如何被長期監督」，與一次執行無關。

## 歷史 (Run History)

| 術語 (Term) | 英文 (English) | 定義 (Definition) | 注意事項 |
| --- | --- | --- | --- |
| 帳本 | Journal | append-only JSONL；`一筆完成的 run 一行` | `帳本記完成的, daemon 報進行中的` |
| 落空觸發 | Skipped Fire | cron 觸發時上一輪還在跑，該次被丟棄 | 仍會留下 `cron_skip` 紀錄 |
| 未知結束碼 | Unknown Exit Code | 落檔為 `null` 而`非 0` | spawn 失敗、或被信號終止時無自己的 code |
| 管理埠 | Admin Port | `0.0.0.0:8502`，dashboard 與 webhook 共用；號碼取 internal 段 | `無認證`；`LAN 可達`但無 tunnel，可達即等同於 shell 存取 |
| 同源檢查 | Same-Origin Guard | 帶 `Origin` 且與 `Host` 不符者一律 403 | 綁定位址不是邊界；這才是擋瀏覽器跨站觸發的東西 |

## 執行語意 (Execution Semantics)

| 術語 (Term) | 英文 (English) | 定義 (Definition) | 出處 (Source) |
| --- | --- | --- | --- |
| 常駐任務 | Long-running Task | 期望持續存活的行程，退出時由 auto-restart 拉回 | `autorestart: true` |
| 一次性排程任務 | One-shot Scheduled Task | 由 cron 觸發、執行完即結束的任務；慣例組合為 `cron_restart` + `autorestart:false` + `max_retries:0` | `dux/ecosystem.config.js` |
| 自動重啟 | Auto-restart | 行程異常結束後自動拉起的行為 | `README.md` Auto-restart behaviour |
| 監看 | Watch | 檔案變更時重啟任務的行為 | `executor/`、`fsnotify` |
| 復甦 | Resurrect | 由 dump 檔還原上次儲存的任務清單 | `pm2 resurrect`、`~/.config/pm2/dump.json` |
| 儲存 | Save | 把目前任務清單寫入 dump 檔 | `pm2 save` |
| 開機啟動 | Startup | 把 daemon 註冊到作業系統開機流程 | `pm2 startup` |

## 架構 (Architecture)

| 術語 (Term) | 英文 (English) | 定義 (Definition) | 出處 (Source) |
| --- | --- | --- | --- |
| 常駐程式 | Daemon | 持有所有行程狀態的長期行程 | `server.go` |
| 命令列客戶端 | CLI Client | 薄 RPC 客戶端；`本身不持有任何狀態` | `cmd/` |
| Unix socket | Unix Socket | CLI 與 daemon 之間的傳輸層 | `network/` |
| 狀態目錄 | State Directory | `~/.config/pm2/`，首次執行自動建立 | `README.md` State files |
| 傾印檔 | Dump File | `~/.config/pm2/dump.json`，任務清單的持久化形式 | `process_manager.go` |

## 介面 (Interfaces)

| 術語 (Term) | 英文 (English) | 定義 (Definition) | 出處 (Source) |
| --- | --- | --- | --- |
| 精靈 | Wizard | 互動式建立 ecosystem 設定的流程 | `pm2 wizard` / `pm2 w` |
| 監視器 | Monitor | 系統活動即時儀表 | `pm2 monitor` / `pm2 m` |
| 工作流命令 | Workflow Command | workflow 的四個動詞 | `pm2 workflow list/run/runs/show`，`無短別名` |
| 網頁儀表 | Web Dashboard | 瀏覽器介面，隨 daemon 啟動 | `pm2 web` 只負責印出並開啟網址 |
| 日誌監視 | Log Monitor | 多任務日誌的即時彙整檢視 | `pm2 logs monitor` |
| 任務管理器 | Task Manager | 任務層級的互動管理介面 | `pm2 taskmanager` / `pm2 tm` |

## 設定 (Configuration)

| 術語 (Term) | 英文 (English) | 定義 (Definition) | 注意事項 |
| --- | --- | --- | --- |
| 生態設定 | Ecosystem Config | 描述一組應用的設定檔 | `ecosystem.config.json` 為建議格式 |
| JS 設定 | JS Ecosystem Config | `.js` 形式的設定 | 本實作以 `goja` 執行，`不是 Node`：沒有 `require` / `__dirname` / `process`，路徑只能寫字面值 |
| 合併設定 | Merged Configuration | `pm2 config` 顯示的最終生效設定 | `pm2 config --source` 可看來源 |
| 註冊動作 | Apply | 把 ecosystem config 載入 daemon 的動作 | 是 `pm2 apply`，`不是` `pm2 start` |

> `常見誤用`：在 `ecosystem.config.js` 中使用 `require`、`__dirname` 或
> `process.env`，以及把 `args` 寫成字串而非 `[]string`。兩者都會在載入時失敗。
>
> `workflow 的常見誤用`：把 `args` / `env` / `cwd` 寫在 `task:` 或 `workflow:`
> stage 上（`載入即失敗`，不是靜默忽略）；以為 stage 會出現在 `pm2 list`
> （不會，它不進 registry）；以為失敗的 stage 會被 `max_restarts` 重試（不會）。
