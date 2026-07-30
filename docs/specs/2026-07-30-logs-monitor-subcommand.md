# Logs Monitor Subcommand Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the Interactive Log Explorer from `pm2 monitor logs` / `pm2 m logs` to `pm2 logs monitor` / `pm2 logs m` without changing the existing monitor dashboard command or its `m` alias.

**Architecture:** Root `LogsCmd` remains the streaming command when invoked directly and becomes the parent of one interactive `LogsMonitorCmd` child. `MonitorCmd` remains an independent dashboard root with aliases `m` and `dashboard`; it no longer owns any log-browser child. The Bubble Tea model and log streaming API are unchanged.

**Tech Stack:** Go 1.26.3, Cobra, Bubble Tea, standard `testing`.

**Status:** Completed and verified on 2026-07-30.

> **Current layout contract:** The command placement in this spec remains
> current. Its full-screen Tree/Viewer presentation is superseded by
> [`2026-07-30-log-browser-split-pane.md`](./2026-07-30-log-browser-split-pane.md):
> the Tree remains on the left, the loaded log remains on the right, and Enter
> focuses Viewer navigation.

## Global Constraints

- Use Traditional Chinese plus English terminology in user-facing collaboration.
- Keep one file one responsibility and one package/folder one domain.
- Preserve all existing uncommitted log-streaming/Tree Explorer work.
- Do not modify `go.mod` or `go.sum`.
- `pm2 logs [target]` remains continuous streaming mode.
- `pm2 logs monitor [target]` and `pm2 logs m [target]` are the only Interactive Log Explorer paths.
- `pm2 monitor`, `pm2 m`, and `pm2 dashboard` remain the existing process dashboard.
- Do not retain `pm2 monitor logs` or `pm2 m logs` compatibility paths.
- Do not commit, push, or open a pull request unless explicitly requested.

---

### Task 1: Reparent the Interactive Command

**Files:**

- Modify: `cmd/logs_test.go`
- Modify: `cmd/monitor_test.go`
- Modify: `tui/views/log_browser_test.go`
- Move: `cmd/monitor_logs.go` to `cmd/logs_monitor.go`
- Modify: `tui/views/log_browser.go`

**Interfaces:**

- Keeps: `LogsCmd.RunE` as streaming mode.
- Produces:

```go
var LogsMonitorCmd = &cobra.Command{
	Use:     "monitor [name]",
	Aliases: []string{"m"},
}
```

- Keeps unchanged: `MonitorCmd.Use == "monitor"` and `MonitorCmd.Aliases` containing `"m"` and `"dashboard"`.

- [x] **Step 1: Write failing command-tree tests**

Add assertions:

```go
if got := LogsMonitorCmd.Parent(); got != LogsCmd {
	t.Fatalf("LogsMonitorCmd.Parent() = %v, want LogsCmd", got)
}
for _, path := range [][]string{{"logs", "monitor"}, {"logs", "m"}} {
	command, _, err := Cmd.Find(path)
	if err != nil || command != LogsMonitorCmd {
		t.Fatalf("pm2 %s must resolve to LogsMonitorCmd", strings.Join(path, " "))
	}
}
for _, child := range MonitorCmd.Commands() {
	if child.Name() == "logs" {
		t.Fatal("MonitorCmd must not own a logs child")
	}
}
```

Keep the existing assertions that `pm2 monitor` defaults to the detail dashboard and that root alias `pm2 m` resolves to `MonitorCmd`.

- [x] **Step 2: Write the failing TUI title test**

Change the Tree Explorer rendering assertion from `pm2 monitor logs` to:

```go
if !strings.Contains(output, "pm2 logs monitor") {
	t.Fatalf("output missing logs monitor title: %q", output)
}
```

- [x] **Step 3: Run tests and verify RED**

Run:

```bash
go test ./cmd ./tui/views -run 'TestLogsMonitor|TestMonitor|TestRenderLogBrowserTreeExplorer' -count=1
```

Expected: FAIL because `LogsMonitorCmd` does not exist, the browser is still parented by `MonitorCmd`, and the TUI title still says `pm2 monitor logs`.

- [x] **Step 4: Reparent and rename the command**

Move `cmd/monitor_logs.go` to `cmd/logs_monitor.go`, rename the exported command to `LogsMonitorCmd`, set `Use` to `monitor [name]`, add alias `m`, and register it with:

```go
func init() {
	LogsCmd.AddCommand(LogsMonitorCmd)
}
```

Do not modify `MonitorCmd`.

- [x] **Step 5: Update the TUI title**

Render `pm2 logs monitor` in `renderLogBrowserHeader`. Do not change Tree, Viewer, paging, or deletion behavior.

- [x] **Step 6: Run focused tests and verify GREEN**

Run:

```bash
go test ./cmd ./tui/views -run 'TestLogsMonitor|TestMonitor|TestRenderLogBrowser' -count=1
```

Expected: PASS.

### Task 2: Synchronize Canonical Documentation and Verify

**Files:**

- Modify: `README.md`
- Modify: `CLAUDE.md`
- Modify: `README.todo`
- Modify: `docs/usage.md`
- Modify: `skills/pm2/SKILL.md`
- Modify: `docs/specs/2026-07-30-log-streaming-navigation.md`
- Move after verification: `plans/2026-07-30-logs-monitor-subcommand.md` to `docs/specs/2026-07-30-logs-monitor-subcommand.md`

**Interfaces:**

- Documents:

```text
pm2 logs [target]          continuous stream
pm2 logs monitor [target]  Interactive Log Explorer
pm2 logs m [target]        Interactive Log Explorer alias
pm2 monitor / pm2 m        existing process dashboard
```

- [x] **Step 1: Establish the PM2 skill RED baseline**

Run:

```bash
rg -n 'pm2 monitor logs|pm2 m logs|pm2 logs monitor|pm2 logs m' skills/pm2/SKILL.md
```

Expected: the reference contains the old `monitor logs` paths and no new `logs monitor` paths.

- [x] **Step 2: Update canonical documentation**

Replace the old interactive command path in current README, usage, CLAUDE,
README.todo, and PM2 skill content. Add a superseded-command pointer to the
previous streaming/navigation spec; the new archived spec owns the final
placement. Keep root `monitor` / `m` dashboard wording intact.

- [x] **Step 3: Validate command help and PM2 skill**

Run:

```bash
go run . logs --help
go run . logs monitor --help
go run . logs m --help
go run . monitor --help
uv run --with pyyaml python /Users/shuk/.codex/skills/.system/skill-creator/scripts/quick_validate.py skills/pm2
```

Expected: `logs` help lists `monitor`; both child paths show Interactive Log Explorer help; `monitor` remains the dashboard; skill validation prints `Skill is valid!`.

- [x] **Step 4: Run the complete Go verification suite**

Run:

```bash
gofmt -w cmd/logs_monitor.go cmd/logs_test.go cmd/monitor_test.go tui/views/log_browser.go tui/views/log_browser_test.go
go test ./... -count=1
go test -race ./... -count=1
go build ./...
go vet ./...
git diff --check
```

Expected: every command exits 0.

- [x] **Step 5: Archive the completed plan and audit scope**

Move this file to `docs/specs/2026-07-30-logs-monitor-subcommand.md`, mark every
step complete, and run:

```bash
git status --short
git diff --name-only
git diff -- go.mod go.sum
git diff --check
```

Expected: the existing feature work plus this command-placement follow-up are
present; `go.mod` and `go.sum` remain untouched.
