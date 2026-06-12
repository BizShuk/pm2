# Tasks

- `[ ]` Add `cron` field to config and protocol definitions
    - `[ ]` Update `AppConfig` in `config/ecosystem.go`
    - `[ ]` Update `AppStartReq` in `daemon/protocol.go`
    - `[ ]` Update `ProcessInfo` and `DumpEntry` in `process/types.go`
- `[ ]` Implement cron schedule logic in daemon
    - `[ ]` Add `CronTriggered` execution logic and ID reuse in `daemon/server.go`
    - `[ ]` Add `triggerCron` helper in `daemon/server.go`
    - `[ ]` Add nil checks to `stopProcess` in `daemon/server.go`
    - `[ ]` Propagate `Cron` settings in `restartByName`, `save`, and `resurrect`
- `[ ]` Implement CLI & UI updates
    - `[ ]` Add `--cron` flag in `cmd/start.go`
    - `[ ]` Update monitor view in `tui/model.go` to display schedule cron tasks
- `[ ]` Verification
    - `[ ]` Run unit tests with `go test ./...`
    - `[ ]` Manually test schedule-triggered cron task behavior
