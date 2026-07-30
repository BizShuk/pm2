# Log Streaming and Tree Navigation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `pm2 logs` a continuous stdout/stderr stream, move the interactive file browser to `pm2 monitor logs` / `pm2 m logs`, expose a public Go channel API, and use Tree Explorer Left/Right plus Log Viewer PageUp/PageDown navigation.

**Architecture:** The existing `logfile` package remains the sole managed-log domain and gains a channel-based follower that emits typed entries. Root `cmd/logs.go` consumes that API and routes stdout/stderr entries to the matching command writer; `cmd/monitor_logs.go` owns the interactive Bubble Tea subcommand. `tui/logbrowser` is reduced to a Tree Explorer, Viewer, and delete-confirm state machine with a focused `tree.go` projection.

**Tech Stack:** Go 1.26.3, Bubble Tea, Lip Gloss, Cobra, standard `context` and filesystem APIs.

**Status:** Completed and verified on 2026-07-30.

> **Current contracts:** The channel and CLI streaming design in this spec
> remains current. Its Interactive Log Explorer command placement is
> superseded by
> [`2026-07-30-logs-monitor-subcommand.md`](./2026-07-30-logs-monitor-subcommand.md):
> use `pm2 logs monitor [target]` or `pm2 logs m [target]`; `pm2 monitor` and
> `pm2 m` remain the process dashboard. Its full-screen navigation layout is
> superseded by
> [`2026-07-30-log-browser-split-pane.md`](./2026-07-30-log-browser-split-pane.md).

## Global Constraints

- Use Traditional Chinese plus English terminology in user-facing collaboration.
- Keep one file one responsibility and one package/folder one domain.
- Preserve unrelated worktree changes and do not modify `go.mod` or `go.sum`.
- `pm2 logs [target]` is streaming mode and does not enter an alternate screen.
- `pm2 monitor logs [target]` and `pm2 m logs [target]` are the interactive mode.
- Stream formatting is exactly `[YYYY-MM-DD HH:MM:SS] <app_name> | <log>`.
- Stream stdout sources to command stdout and stderr sources to command stderr.
- Tree navigation uses `→` to expand/open and `←` to collapse/back; do not retain Enter/Esc as duplicate navigation paths.
- Permit deletion only for a selected log-file row in the Tree Explorer, behind explicit `y/N` confirmation.
- In the Log Viewer, keep line movement, add PageUp/PageDown page movement, and use `←` to return to Tree Explorer.
- Do not commit, push, or open a pull request unless explicitly requested.

---

### Task 1: Public Channel-Based Log Follower

**Files:**

- Create: `logfile/entry.go`
- Create: `logfile/follow.go`
- Create: `logfile/follow_test.go`

**Interfaces:**

- Consumes: current stdout/stderr files written by `logfile.Writer`.
- Produces:

```go
type Stream string

const (
	StreamStdout Stream = "stdout"
	StreamStderr Stream = "stderr"
)

type Source struct {
	AppName string
	Path    string
	Stream  Stream
}

type Entry struct {
	Time    time.Time
	AppName string
	Stream  Stream
	Message string
}

func (e Entry) String() string
func Follow(ctx context.Context, sources []Source) (<-chan Entry, <-chan error)
```

- [x] **Step 1: Write failing entry-format tests**

Add:

```go
func TestEntryStringIncludesTimeApplicationAndMessage(t *testing.T) {
	entry := Entry{
		Time:    time.Date(2026, 7, 30, 8, 9, 10, 0, time.Local),
		AppName: "worker",
		Message: "completed job",
	}
	if got, want := entry.String(),
		"[2026-07-30 08:09:10] worker | completed job"; got != want {
		t.Fatalf("Entry.String() = %q, want %q", got, want)
	}
}
```

- [x] **Step 2: Write failing channel-follow tests**

Create a timestamped current log, call `Follow`, append a complete line after `Follow` returns, and assert the receive-only channel emits one parsed `Entry`. Also test:

- stdout/stderr source identity is preserved.
- existing bytes are not replayed; following starts at current EOF.
- a removed/recreated path is reopened and new lines are emitted.
- a partial line is held until its terminating newline arrives.
- cancelling `ctx` closes both returned channels.

- [x] **Step 3: Run focused tests and verify RED**

Run:

```bash
go test ./logfile -run 'TestEntry|TestFollow' -count=1
```

Expected: FAIL because `Entry`, `Source`, `Stream`, and `Follow` do not exist.

- [x] **Step 4: Implement entry parsing and formatting**

`entry.go` owns the value model and formatting. Parse stored writer lines of the form `[YYYY-MM-DD HH:MM:SS] message`; preserve their timestamp and remove the stored prefix from `Message`. For legacy unprefixed lines, use the observation time and retain the complete line as `Message`.

- [x] **Step 5: Implement filesystem following**

`follow.go` owns per-source offset, file identity, pending partial-line bytes, and polling. Initial existing files begin at EOF. A new or replaced path begins at byte zero; a truncated path resets its offset and pending bytes. Every send and poll waits on `ctx.Done()` so cancellation cannot leak goroutines. Close both output channels exactly once when the follower exits.

- [x] **Step 6: Run focused tests and verify GREEN**

Run:

```bash
go test ./logfile -run 'TestEntry|TestFollow' -count=1
```

Expected: PASS.

### Task 2: Streaming Root Command and Interactive Monitor Subcommand

**Files:**

- Modify: `cmd/logs.go`
- Create: `cmd/logs_stream.go`
- Create: `cmd/monitor_logs.go`
- Modify: `cmd/logs_test.go`
- Modify: `cmd/monitor_test.go`

**Interfaces:**

- Consumes: daemon `CmdList`, `process.ProcessInfo.LogFile`, `process.ProcessInfo.ErrorFile`, and `logfile.Follow`.
- Produces:

```text
pm2 logs [name|id|namespace|namespace:name]          continuous stream
pm2 monitor logs [name|id|namespace|namespace:name] interactive browser
pm2 m logs [name|id|namespace|namespace:name]       interactive browser
```

- [x] **Step 1: Write failing Cobra contract tests**

Assert:

```go
if !strings.Contains(strings.ToLower(LogsCmd.Short), "stream") {
	t.Fatalf("root logs must describe streaming mode")
}
if got := MonitorLogsCmd.Parent(); got != MonitorCmd {
	t.Fatalf("MonitorLogsCmd.Parent() = %v, want MonitorCmd", got)
}
command, _, err := Cmd.Find([]string{"m", "logs"})
if err != nil || command != MonitorLogsCmd {
	t.Fatalf("pm2 m logs must resolve to MonitorLogsCmd")
}
```

Also assert root `LogsCmd` no longer imports or starts Bubble Tea and `MonitorLogsCmd` retains `logs [name]`.

- [x] **Step 2: Write failing stream-routing tests**

Feed deterministic `logfile.Entry` values to the CLI consumer and assert:

```go
stdoutEntry := logfile.Entry{Stream: logfile.StreamStdout, Message: "out", ...}
stderrEntry := logfile.Entry{Stream: logfile.StreamStderr, Message: "err", ...}
```

The formatted stdout entry appears only in `stdout`; the formatted stderr entry appears only in `stderr`. A follower error terminates the command with context while normal context cancellation returns without treating Ctrl+C as a stream failure.

- [x] **Step 3: Run focused tests and verify RED**

Run:

```bash
go test ./cmd -run 'TestLogs|TestMonitorLogs|TestWriteLogStream' -count=1
```

Expected: FAIL because the root command is still interactive and `MonitorLogsCmd` plus stream routing do not exist.

- [x] **Step 4: Implement the streaming command shell**

`cmd/logs.go` owns only Cobra metadata and signal-aware invocation. It lists the daemon snapshot, resolves the optional target using the existing ID/name/namespace/composite contract, builds one stdout and one stderr `logfile.Source` per matched application path, starts `logfile.Follow`, and passes the returned channels to `writeLogStream`.

`writeLogStream` writes `Entry.String()+"\n"` to `cmd.OutOrStdout()` or `cmd.ErrOrStderr()` based on `Entry.Stream`.

- [x] **Step 5: Move the Bubble Tea browser under monitor**

`cmd/monitor_logs.go` defines `MonitorLogsCmd`, starts:

```go
tea.NewProgram(
	logbrowser.New(cliruntime.SocketPath(), target),
	tea.WithAltScreen(),
)
```

and registers it through `MonitorCmd.AddCommand(MonitorLogsCmd)`. Do not duplicate a root interactive alias.

- [x] **Step 6: Run focused and package tests and verify GREEN**

Run:

```bash
go test ./cmd ./logfile -count=1
```

Expected: PASS.

### Task 3: Interactive Tree Explorer State and Navigation

**Files:**

- Create: `tui/logbrowser/tree.go`
- Modify: `tui/logbrowser/model.go`
- Modify: `tui/logbrowser/commands.go`
- Modify: `tui/logbrowser/keys.go`
- Modify: `tui/logbrowser/model_test.go`

**Interfaces:**

- Consumes: `process.ProcessInfo`, `logfile.FileInfo`, `loadFiles`, `loadFile`, and `deleteFile`.
- Produces: `treeRow`, `Model.visibleTreeRows() []treeRow`, `Model.treeItems() []string`, and the `screenTree → screenViewer → screenTree` transition contract.

- [x] **Step 1: Write failing Tree Explorer navigation tests**

Add tests that construct a model with one application and assert:

```go
m, cmd := updateKey(t, m, "right")
if cmd == nil {
	t.Fatal("Right on application must discover its log files")
}
m = updateMessage(t, m, cmd())
if got := len(m.visibleTreeRows()); got != 2 {
	t.Fatalf("visible rows = %d, want application plus file", got)
}

m, _ = updateKey(t, m, "down")
m, cmd = updateKey(t, m, "right")
if m.screen != screenViewer || cmd == nil {
	t.Fatal("Right on file must open the Log Viewer")
}

m, _ = updateKey(t, m, "left")
if m.screen != screenTree {
	t.Fatalf("Left from viewer = %v, want tree", m.screen)
}
m, _ = updateKey(t, m, "left")
if got := len(m.visibleTreeRows()); got != 1 {
	t.Fatalf("Left on file leaves %d rows, want collapsed application", got)
}
```

Also assert that `d` is ignored on an application row and opens confirmation on a file row.

- [x] **Step 2: Run focused tests and verify RED**

Run:

```bash
go test ./tui/logbrowser -run 'TestTree|TestDelete' -count=1
```

Expected: FAIL because `screenTree`, `visibleTreeRows`, and Left/Right Tree navigation do not exist.

- [x] **Step 3: Implement the minimal Tree projection**

Create `tree.go` with focused row metadata:

```go
type treeRowKind uint8

const (
	treeApplication treeRowKind = iota
	treeFile
)

type treeRow struct {
	kind             treeRowKind
	applicationIndex int
	fileIndex        int
}
```

Implement `visibleTreeRows`, `selectedTreeRow`, `selectedApplication`, `selectedFilePath`, and `treeItems`. Application rows use `▸` when collapsed and `▾` when expanded; file rows are indented beneath their owning application.

- [x] **Step 4: Replace application/file screens with Tree state**

In `model.go`:

```go
const (
	screenTree screen = iota
	screenViewer
	screenConfirmDelete
)
```

Store `treeCursor`, `expanded map[int]bool`, and `filesByApplication map[int][]logfile.FileInfo`. Make `filesMsg` and `deletedMsg` carry `applicationIndex`, cache results for the correct application, and refresh only that application's file list after deletion.

- [x] **Step 5: Implement Right/Left navigation**

In `keys.go`:

```go
case "right":
	return m.moveIn()
case "left":
	return m.moveOut()
```

`moveIn` expands an application or opens a file. `moveOut` returns Viewer to Tree; on a file row it selects and collapses the parent application; on an expanded application it collapses it. Remove Enter/Esc as navigation aliases, retaining Esc only to cancel delete confirmation.

- [x] **Step 6: Run focused and package tests and verify GREEN**

Run:

```bash
go test ./tui/logbrowser -count=1
```

Expected: PASS.

### Task 4: Viewer Paging and Screen-Specific Key Hints

**Files:**

- Modify: `tui/logbrowser/keys.go`
- Modify: `tui/logbrowser/model_test.go`
- Modify: `tui/views/log_browser.go`
- Modify: `tui/views/log_browser_test.go`

**Interfaces:**

- Consumes: `Model.height` and the renderer's `bodyHeight := max(1, height-3)`.
- Produces: PageUp/PageDown cursor movement by `max(1, height-3)` lines and screen-specific footer hints.

- [x] **Step 1: Write failing paging and footer tests**

Add a Viewer test with ten lines and `height: 8`, then assert:

```go
m, _ = updateKey(t, m, "pgup")
if m.lineCursor != 4 {
	t.Fatalf("PageUp cursor = %d, want 4", m.lineCursor)
}
m, _ = updateKey(t, m, "pgdown")
if m.lineCursor != 9 {
	t.Fatalf("PageDown cursor = %d, want 9", m.lineCursor)
}
```

Render Tree and Viewer contexts and assert the Tree footer contains `←`, `→`, and conditional `d delete`, while the Viewer footer contains `PgUp/PgDn` and `← back` but not `delete`.

- [x] **Step 2: Run focused tests and verify RED**

Run:

```bash
go test ./tui/logbrowser ./tui/views -run 'TestViewerPage|TestRenderLogBrowser' -count=1
```

Expected: FAIL because paging and the new footer contracts are absent.

- [x] **Step 3: Implement page-sized movement**

Add:

```go
func (m *Model) movePage(direction int) {
	pageSize := max(1, m.height-3)
	m.lineCursor = clampIndex(m.lineCursor+direction*pageSize, len(m.lines))
}
```

Handle Bubble Tea key strings `pgup` and `pgdown` only on `screenViewer`.

- [x] **Step 4: Render screen-specific footer hints**

Use these canonical actions:

```text
Tree:   ↑↓ / jk navigate  │  ← collapse/back  │  → expand/open  │  d delete  │  q quit
Viewer: ↑↓ / jk line      │  PgUp/PgDn page   │  ← back         │  q quit
```

Only include `d delete` when the selected Tree row is a file.

- [x] **Step 5: Run focused and package tests and verify GREEN**

Run:

```bash
go test ./tui/logbrowser ./tui/views -count=1
```

Expected: PASS.

### Task 5: Canonical Documentation and Full Verification

**Files:**

- Modify: `README.md`
- Modify: `CLAUDE.md`
- Modify: `README.todo`
- Modify: `docs/usage.md`
- Modify: `skills/pm2/SKILL.md`
- Move after completion: `plans/2026-07-30-log-streaming-navigation.md` to `docs/specs/2026-07-30-log-streaming-navigation.md`

**Interfaces:**

- Consumes: verified streaming and interactive behavior from Tasks 1–4.
- Produces: one canonical command/API contract and synchronized project/skill documentation.

- [x] **Step 1: Establish the skill-document RED baseline**

Run:

```bash
rg -n 'pm2 logs|application list|log file list|Enter|Esc|page|delete' skills/pm2/SKILL.md
```

Observed: the skill describes root `pm2 logs` as the interactive Enter/Esc browser and has no channel-streaming or PageUp/PageDown contract.

- [x] **Step 2: Update canonical documentation**

Document:

```text
pm2 logs [target]                         → continuous stdout/stderr stream
pm2 monitor logs [target] / pm2 m logs   → Tree Explorer ⇄ Log Viewer
logfile.Follow(ctx, sources)              → receive-only Entry/error channels
```

State that streamed entries format as `[datetime] app_name | log`; Right expands/opens; Left collapses/returns; `d` is Tree-file-only with `y/N`; and PageUp/PageDown moves a page in the Viewer. Keep architecture/API ownership in `CLAUDE.md`; keep user workflows in `README.md` and `docs/usage.md`; record the completed result in `README.todo`.

- [x] **Step 3: Validate the synchronized PM2 skill**

Run:

```bash
uv run --with pyyaml python /Users/shuk/.codex/skills/.system/skill-creator/scripts/quick_validate.py skills/pm2
```

Expected: `Skill is valid!`

- [x] **Step 4: Format and run the complete Go verification suite**

Run:

```bash
gofmt -w logfile/entry.go logfile/follow.go logfile/follow_test.go cmd/logs.go cmd/logs_stream.go cmd/monitor_logs.go cmd/logs_test.go cmd/monitor_test.go tui/logbrowser/model.go tui/logbrowser/tree.go tui/logbrowser/commands.go tui/logbrowser/keys.go tui/logbrowser/model_test.go tui/views/log_browser.go tui/views/log_browser_test.go
go test ./...
go test -race ./...
go build ./...
go vet ./...
git diff --check
```

Expected: every command exits 0.

- [x] **Step 5: Run streaming and interactive PTY smoke tests**

Build a temporary binary and start a temporary daemon/process with isolated config/log paths.

1. Exercise `logfile.Follow` plus the `pm2 logs` channel consumer in focused
   tests, emit new stdout and stderr lines, and verify each continues to the
   correct writer with datetime plus app name. The root command's fixed
   real-home socket is not invoked during isolated verification.
2. Run the same `logbrowser.Model` used by `pm2 m logs` against a custom Unix
   socket in an actual PTY and exercise:

```text
Right expand → Down → Right open → PageUp → PageDown → Left back
→ d → n → d → y
```

Verify the first confirmation preserves the file, the second removes it, and cleanup removes every temporary process/file.

- [x] **Step 6: Archive the completed plan and audit scope**

Move this file to `docs/specs/2026-07-30-log-streaming-navigation.md`, update any cross-reference to the final path, then run:

```bash
git status --short
git diff --stat
git diff --name-only
git diff --check
```

Expected: only streaming, interactive Tree navigation, tests, and owned documentation are changed; `go.mod` and `go.sum` remain untouched.
