# TUI Monitor Bright Highlight Design

Provide a bright highlight style for the current selected process in both `pm2 m` (dual-pane dashboard) and `pm2 m -d` (single-pane process list dashboard).

## Visual Theme (Scheme B: Indigo & Nordic Frost Blue)

The selected process row will feature:

- Background: Indigo/Nordic Frost Blue background (`clSelBg`).
- Process Name: Bold text with bright cyan color (`clSelName`).
- General Fields: Bright text color (`clSelText`) for high contrast on the selection background.
- Status/ID Fields: Retain status colors but rendered with bold styling.

### Styling Variables

Modify/Add the following style variables in `tui/model.go`:

```go
clSelBg   = lipgloss.AdaptiveColor{Light: "#e0e7ff", Dark: "#2e3440"} // Indigo / Nord Frost Blue
clSelName = lipgloss.AdaptiveColor{Light: "#0891b2", Dark: "#06b6d4"} // Bright Cyan
clSelText = lipgloss.AdaptiveColor{Light: "#0f172a", Dark: "#ffffff"} // Dark Slate / Bright White
```

## Proposed Changes

### Dual-pane Dashboard (`pm2 m -d` / `buildLeft`)

In `buildLeft(w, h int)`:

- When rendering the selected row (`i == m.selected`):
    - Apply `clSelName` with `Bold(true)` to the process name.
    - Apply `clSelText` to the uptime instead of `clMuted`.

### Single-pane List Dashboard (`pm2 m` / `buildListTUI`)

In `buildListTUI()`:

- When rendering cells for the selected row (`i == m.selected`):
    - For the `name` column: Apply `clSelName` with `Bold(true)`.
    - For other general metadata columns (`namespace`, `version`, `pid`, `user`, `cron`, `last exec`): Apply `clSelText`.
    - For status columns (`id`, `status`): Keep status color, but make it `Bold(true)`.
    - For metrics columns (`cpu`, `mem`): If online, keep green/text but bold; if offline, fallback to `clSelText`.

## Verification Plan

### Manual Verification

- Run `pm2 m` and check that the selected row uses the new color theme and bold styling.
- Run `pm2 m -d` and check that the selected row uses the new color theme and bold styling.
- Verify status colors (online/errored/stopped) are still correctly visible and readable when selected.
