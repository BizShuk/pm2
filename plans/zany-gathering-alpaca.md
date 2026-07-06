# TUI Namespace Switcher — `pm2 monit`

## Context

`pm2 monit -d` currently shows every running managed process in a single flat list. Users running multiple namespaces (e.g. `prod`, `staging`, `dev`) have to mentally scan and group rows to focus on one environment. We want left/right arrow keys to switch the dashboard between a synthetic `All` view and each individual namespace, with a tab/chip strip rendered at the top of the TUI so users can see which namespace is active and where they are within the list.

The change is purely client-side in the TUI; the daemon still returns the full process list as it does today. Filtering happens after the RPC list response, and the namespace strip is recomputed from the full set on every refresh so it stays accurate as processes come and go.

## Output of this plan

A new one-line namespace strip rendered between the existing `pm2 monit` title bar and the process list (either wide-table or two-pane detail). `←`/`→` cycle through namespaces; the selected chip is highlighted; only processes whose `Namespace` matches the selected chip are shown. `All` is always the leftmost chip and shows the entire list.

## Files to modify

| File                             | Why                                                                                                                                                                            |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `tui/model.go`                   | Add `namespaces`, `nsCursor`, `allProcs` fields; key handlers for `←`/`→`; helpers to recompute the chip set and filter the visible list; wire into `applyRefresh` and `View`. |
| `tui/views/context.go`           | Extend `ViewContext` with `Namespaces []string`, `NsCursor int` so the renderer can render the strip without a back-reference to `tui.Model`.                                  |
| `tui/views/namespace.go` _(new)_ | `RenderNamespaceBar(ctx, width)` — pure function for the chip strip with sliding-window behaviour.                                                                             |
| `tui/views/layout.go`            | Slot the new bar into `RenderLayout` between `RenderHeader` and the body; reduce body/content height by 1.                                                                     |
| `tui/views/list.go`              | Account for the extra row used by the namespace strip when computing the wide-table body's vertical space and when constructing the empty-state message.                       |
| `tui/model_test.go`              | Unit tests for namespace set rebuild, filter application, key handling, and persistence of cursor across refresh.                                                              |

## Design

### Controller (`tui/model.go`)

Add three fields to `Model`:

```go
allProcs   []process.ProcessInfo // unfiltered list, source of truth
namespaces []string              // ["All"] + unique sorted namespaces from allProcs
nsCursor   int                   // index into namespaces; 0 == All
```

Helpers:

- `recomputeNamespaces()` — rebuild `m.namespaces` from `m.allProcs` (collect unique namespaces, sort, prepend `"All"`).
- `applyNamespaceFilter()` — overwrite `m.procs` with the filtered subset of `m.allProcs` according to `m.namespaces[m.nsCursor]`, then call `m.sortProcs(...)` to keep ordering consistent. If selection is out of range after filter, clamp to 0.
- `cycleNamespace(delta int)` — update `m.nsCursor` (modulo `len(m.namespaces)`) and call `applyNamespaceFilter`.

`applyRefresh` becomes:

1. Save previous `procs[m.selected].ID` (selection preservation).
2. `m.allProcs = msg.procs`.
3. `recomputeNamespaces()` — note: this needs to happen on the previous `nsCursor` so a refresh doesn't lose state. Specifically: recompute, then if `m.nsCursor >= len(m.namespaces)`, clamp to 0; if `m.namespaces[m.nsCursor]` is no longer present (e.g. last process in that namespace exited), clamp to 0.
4. `applyNamespaceFilter()`.

Key handling — extend `handleKey` for `←`/`→`, **before** the empty-procs bail-out (so navigating between namespaces with an empty filter still works):

```go
case "left":
    m.cycleNamespace(-1)
    return m, nil
case "right":
    m.cycleNamespace(+1)
    return m, nil
```

`View()` passes the new fields into `ViewContext`:

```go
ctx := views.ViewContext{
    ...
    Namespaces: m.namespaces,
    NsCursor:   m.nsCursor,
}
```

(`m.procs` passed to the body is already filtered.)

### View (`tui/views/namespace.go`)

Single new function `RenderNamespaceBar(ctx, w int) string`:

- Compute width of the full string `namespace_a · namespace_b · ...` (with a leading `ns:` label or just a leading space for breathing room — chosen during implementation).
- If total width ≤ `w`, render all chips directly.
- Otherwise, slide a window over `namespaces` that keeps `nsCursor` centred (or near left when on the leftmost). Add `‹` at the start when there's content to the left of the window, and `›` at the end when there's more to the right (only on the side that overflows).
- Selected chip: bold + `SelBg` background + `SelText` foreground (matches the existing selection palette from `tui/views/list.go` and `tui/views/format.go`).
- Unselected chips: `Text` foreground; mild `Muted` for the `·` separator.
- The bar uses the same `HdrBg` background as `RenderHeader` / `RenderFooter` for visual continuity.
- Empty state (`len(namespaces) == 1` → just `"All"`) renders the bare chip.

Sliding-window algorithm (sketch):

```go
// measure render widths with runewidth.StringWidth; cycle = sum(chip) + len-1 separators + arrows.
const sep = " · "
const pad = 1 // 1 space either side

cycle := func(start, end int) string {
    var parts []string
    for _, ns := range namespaces[start:end] { parts = append(parts, ns) }
    return strings.Join(parts, sep)
}

if cycle(0, len(namespaces)) fits { render it; return }

left := nsCursor
for {
    window := cycle(left, right) // expand right until exceeds w, then expand left too
    if fits && ((left == 0) || (right == len(namespaces))) { break }
    ...
}
```

The actually-needed algorithm is: pick `start, end` such that `end-start` is maximal given the width budget and the cursor is inside `[start, end)`. Implementation priority: prefer keeping the cursor visible; when the window equals the chip count on at least one side, drop the corresponding arrow indicator.

### View wiring (`tui/views/layout.go`)

Subtract one row from the body's height budget and slot the bar between header and body:

```go
return lipgloss.JoinVertical(lipgloss.Left,
    RenderHeader(ctx),
    RenderNamespaceBar(ctx, ctx.Width),
    body,
    RenderFooter(ctx.Width, ctx.SortBy),
)
```

That means:

- In `RenderWideTable`, the body now has `ctx.Height - 3` rows (header + namespace bar + footer).
- In the two-pane detail view, `contentH = ctx.Height - 3`, and `RenderLeftPane` / `RenderRightPane` continue to use `contentH`.
- The empty-state branch in `RenderWideTable` (no processes) widens similarly.

### Tests (`tui/model_test.go`)

Add:

- `TestRecomputeNamespaces` — feed mixed namespaces into `allProcs`, expect `["All", "dev", "prod", "staging"]` (sorted, "All" first).
- `TestApplyNamespaceFilter` — `nsCursor=2` ("prod") over a mixed list filters to only `prod` rows; `nsCursor=0` ("All") returns everything.
- `TestCycleNamespaceWraps` — moving right from the last wraps to "All"; left from "All" wraps to the last.
- `TestLeftRightArrowSwitchesNamespace` — sends a `tea.KeyMsg{Type: tea.KeyLeft}` / `tea.KeyRight` and asserts `nsCursor` and `m.procs` move as expected.
- `TestRefreshPreservesNamespaceCursor` — when the active namespace still exists after refresh, the cursor and filter stay; when it disappears (last process in that namespace exited), the cursor falls back to 0 and the list reverts to "All".
- `TestActionKeysStillWorkWithEmptyFilteredView` — pressing `r`/`p`/`d` on an empty filtered list must keep the existing guard (`if len(m.procs) == 0 { return m, nil }`) intact.
- A view test `TestNamespaceBarHighlightsSelected` in either `model_test.go` or a new `tui/views/namespace_test.go`: assert the rendered output contains inverse-style ANSI around the selected chip's label and a `›` (or no `‹`) indicator when the cursor is on the rightmost chip.

## Verification

1. **Unit tests** — `go test -race ./...` (CI gate).
2. **Targeted TUI tests** — `go test ./tui/...` for fast feedback.
3. **Manual smoke** — start the daemon with several processes across 2+ namespaces, e.g.

    ```bash
    ./pm2 start --namespace prod   process1.sh
    ./pm2 start --namespace staging process2.sh
    ./pm2 start --namespace dev    process3.sh
    ./pm2 m -d
    ```

    then exercise: arrow right → "prod" filters to one row; arrow right → "staging" filters to one row; arrow left → back to "prod"; arrow right enough times → wraps to "All"; with a terminal narrow enough to force overflow → confirm `‹`/`›` indicators appear and slide; in "dev" with `killing all dev procs` mid-session → confirm cursor falls back to "All" cleanly.

4. **Backwards check** — `pm2 m` (no `-d`) wide-table mode still renders correctly with the new bar slot.

## Non-goals / explicitly out of scope

- No daemon changes. Filtering is purely client-side.
- No new CLI flag.
- No persistence of the selected namespace across `pm2 monit` invocations — fresh launches always start on "All".
- No changes to `pm2 list` (the non-TUI table command); the chip strip only applies to the live dashboard.
