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
