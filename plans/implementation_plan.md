# Implementation Plan: Schedule-triggered Cron Tasks

We will introduce a new `cron` configuration field. Unlike `cron_restart` (which keeps the process online and restarts it periodically), a `cron` task will be initialized as `stopped` and only executed when the cron schedule triggers. After execution, it returns to the `stopped` state.

## User Review Required

No breaking changes are introduced. The existing `cron_restart` field and behavior will remain untouched.

## Proposed Changes

We will modify five files to add `cron` flag support and scheduler logic.

---

### Config & Protocol Layer

#### [MODIFY] [ecosystem.go](file:///Users/shuk/projects/tmp/pm2/config/ecosystem.go)
- Add `Cron string `json:"cron"`` to `AppConfig` struct.

#### [MODIFY] [protocol.go](file:///Users/shuk/projects/tmp/pm2/daemon/protocol.go)
- Add `Cron string `json:"cron"`` to `AppStartReq` struct.
- Add `CronTriggered bool `json:"cron_triggered"`` to `AppStartReq` struct to differentiate between initial start and scheduled execution.

#### [MODIFY] [types.go](file:///Users/shuk/projects/tmp/pm2/process/types.go)
- Add `Cron string `json:"cron"`` to `ProcessInfo` and `DumpEntry` structs.

---

### Daemon Layer

#### [MODIFY] [server.go](file:///Users/shuk/projects/tmp/pm2/daemon/server.go)
- **`launchProcess`**:
  - Check if `req.Cron != ""` and `!req.CronTriggered`. If true, skip calling `cmd.Start()`, skip background `watchProcess`, and set process status to `process.StatusStopped`.
  - Reuse the existing Process ID if the process name already exists in the process list.
  - Register `req.Cron` to the scheduler, calling a new helper `triggerCron`.
- **`triggerCron`**:
  - Helper to stop any existing instance and call `launchProcess` with `CronTriggered = true`.
- **`stopProcess`**:
  - Add nil checks `if mp.Cmd != nil` to avoid nil pointer panic since `cron` processes start with `Cmd = nil`.
- **`restartByName`**:
  - Propagate `Cron` and set `CronTriggered = true` so restart immediately triggers the execution.
- **`save` & `resurrect`**:
  - Propagate `Cron` to save and restore from config.

---

### CLI & UI Layer

#### [MODIFY] [start.go](file:///Users/shuk/projects/tmp/pm2/cmd/start.go)
- Add `--cron` string flag.
- Bind CLI `--cron` flag to command request structure.
- Print `[id] name scheduled` and print `cron: ...` information when `pid` is 0.

#### [MODIFY] [model.go](file:///Users/shuk/projects/tmp/pm2/tui/model.go)
- Render `cron` trigger configuration and next run details dynamically in the detail panel and table columns.

---

## Verification Plan

### Automated Tests
- Build and run the project locally.
- Run `go test ./...`.

### Manual Verification
1. Run `go run . start ./some_script.sh --cron "* * * * *"`:
   - Verify that the process is registered but **NOT** executed immediately.
   - Verify `pm2 m` shows status `stopped` and displays the next execution time.
   - Verify that it executes every minute, and returns to `stopped` status when finished.
2. Run `pm2 restart <name>` to force immediate run.
3. Verify that `pm2 save` and `pm2 resurrect` correctly restores the `cron` settings.
