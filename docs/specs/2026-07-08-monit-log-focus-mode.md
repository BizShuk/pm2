# log-focus mode for `pm2 monit -d`

## Context

`pm2 monit -d` (alias `pm2 m -d`) opens a two-pane live dashboard. The right pane currently always renders a 17-row detail table plus a small log tail below it. When the user is debugging a single process, the detail block is noise — they want the log tail to fill the whole right pane.

The change: pressing `Enter` on a selected process hides the detail block and shows the log tail at full right-pane height. `Esc` (or `Enter` again) returns to the normal detail+logs view. The mode is a sub-state of the existing two-pane layout; it is only meaningful in `Detail` mode and is a no-op in the wide-table view.

## Design decisions

- **State**: new `logFocus bool` on `Model` and a parallel `LogFocus bool` on `ViewContext`. Orthogonal to `Detail`; the key handler guards with `m.Detail && …` so the wide-table layout is unaffected.
- **Keys**: `enter` toggles log-focus on and off (press Enter again to return to the normal detail+logs view). `esc` also exits log-focus only as a convenience. Both are no-ops in wide-table mode (matching the existing `r/p/d` no-op behavior).
- **Layout**: in log-focus mode, the right pane returns `RenderLogs(p.Name, ctx.Logs, w, h)` directly with the full right-pane height. No detail block above, no height math.
- **Logs re-read**: none on toggle. `m.logs` is already current for the selected process; periodic 2 s refresh keeps it fresh. If logs are mid-load, `RenderLogs` already shows "loading…".
- **Footer hint**: append `{"⏎/esc", "logs only"}` to the keys list in `RenderFooter`. Always visible (matches the always-on style of other keys); harmless in wide-table.

## Critical files

- `tui/model.go` — add `logFocus` field; wire `enter`/`esc` in `handleKey`; plumb `LogFocus` into `ViewContext` in `View()`.
- `tui/views/context.go` — add `LogFocus bool` to `ViewContext`.
- `tui/views/layout.go` — short-circuit `RenderRightPane` on `LogFocus`.
- `tui/views/footer.go` — add one entry to the `keys` slice.
- `tui/model_test.go` — add tests for the toggle and the renderer.

## Implementation steps

### 1. `tui/model.go`

Add a field directly below `Detail bool` (around line 76):

```go
logFocus  bool
```

Add a new branch in `handleKey` (in the action-key switch, after the existing `r/p/d` cases), placed **before** the `len(m.procs) == 0` guard so the keys are always safe to press:

```go
case "enter":
    if m.Detail && len(m.procs) > 0 {
        m.logFocus = !m.logFocus
        return m, nil
    }
case "esc":
    if m.Detail && m.logFocus {
        m.logFocus = false
        return m, nil
    }
```

In `View()` (around line 479), add `LogFocus: m.logFocus` to the `ViewContext` literal.

### 2. `tui/views/context.go`

Add a field next to `Detail bool` (around line 40):

```go
LogFocus   bool   // hide detail block; show only log tail at full height
```

### 3. `tui/views/layout.go`

Update `RenderRightPane` (line 47). After the empty-procs guard, short-circuit on `LogFocus`:

```go
func RenderRightPane(ctx ViewContext, w, h int) string {
    if len(ctx.Procs) == 0 {
        return lipgloss.NewStyle().Width(w).Padding(1, 2).Foreground(theme.Muted).
            Render("no processes\nstart one: pm2 start <script>")
    }
    p := ctx.Procs[ctx.Selected]
    if ctx.LogFocus {
        return RenderLogs(p.Name, ctx.Logs, w, h)
    }
    detail := RenderDetail(p, w)
    if h < 20 {
        return detail
    }
    logH := h - detailRows - 3
    logs := RenderLogs(p.Name, ctx.Logs, w, logH)
    return detail + "\n" + logs
}
```

`RenderLayout` itself is untouched — `contentH` is unchanged, only the right-pane contents differ.

### 4. `tui/views/footer.go`

Add one entry to the `keys` slice (line 15), just before `{"q", "quit"}`:

```go
{"⏎/esc", "logs only"},
```

### 5. Tests in `tui/model_test.go`

Add three tests next to `TestDetailTuiStability`:

- `TestEnterTogglesLogFocus` — `Model{Detail: true, procs: [{...}]}`. Send `tea.KeyMsg{Type: tea.KeyEnter}`; assert `m.logFocus == true`. Send again; assert `m.logFocus == false`. Assert no `tea.Cmd` returned.
- `TestEscExitsLogFocus` — Enter to enter log-focus, then `tea.KeyMsg{Type: tea.KeyEsc}` → `logFocus == false`. Esc from non-log-focus state → no-op. Esc never changes `m.Detail`.
- `TestEnterIsNoopInWideTable` — `Model{Detail: false}`. Send Enter, assert `m.logFocus == false`.
- `TestRenderRightPaneLogFocusHidesDetail` — call `views.RenderRightPane` with `LogFocus: true`, `Width: 50`, `Height: 18`, one proc, one log line. Assert output does NOT contain `"detail —"`, does NOT contain `"script"`, does contain `"logs —"` and `"line 1"`. Also a control case with `LogFocus: false` asserting the output DOES contain `"detail —"`.
- `TestFooterIncludesLogFocusHint` — call `views.RenderFooter(120, "name")`; assert output contains `"⏎/esc"` and `"logs only"`.

## Verification

```bash
cd /Users/bytedance/projects/tmp/pm2

# 1. Build
go build ./...

# 2. Run the TUI test suite
go test ./tui/...

# 3. Manual end-to-end
./pm2 start <some-script>
./pm2 m -d
#   - default view shows detail + log tail on the right
#   - footer shows "⏎/esc  logs only"
#   - press Enter -> detail block disappears, log tail fills the full right pane
#   - press Esc -> detail block reappears
#   - press Enter twice (toggle) -> back to default
#   - press Enter then j/k -> log-focus persists, logs reload for new selection
#   - run `pm2 m` (no -d) -> Enter/Esc are no-ops; hint is informational
```

## Reused functions and patterns

- `m.logs` lifecycle and `readLogs` command: `tui/model.go:431` (existing) — no new log-reading path needed.
- `RenderLogs(name, logs, w, h)`: `tui/views/logs.go:15` (existing) — already handles nil/empty/full-height correctly.
- `RenderRightPane(ctx, w, h)`: `tui/views/layout.go:47` (existing) — extend, don't replace.
- `ViewContext` shape: `tui/views/context.go:25` (existing) — add one field, keep parallel with `Model`.
- Key-handler style: existing `case "r":` / `"p":` / `"d":` cases in `tui/model.go:handleKey` (existing) — mirror their guard pattern.
- `RenderFooter` keys list: `tui/views/footer.go:15` (existing) — append one entry.
