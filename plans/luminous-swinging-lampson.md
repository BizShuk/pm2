# Plan: `pm2 eco` Interactive Ecosystem Wizard

## Context

`pm2 start` 已經能載入 `ecosystem.config.js` / `.json`(見 `config/ecosystem.go:121-159`),但使用者目前必須手寫這個檔案。對新使用者而言,要記住 `module.exports = { apps: [...] }` 結構、`AppConfig` 16 個欄位、JSON tag 命名(`cron_restart` 而非 `cronRestart`)是有摩擦的。

目標:新增 `pm2 eco` 指令(別名 `init` / `wizard`),以互動問答方式引導使用者建立一份合法的 `ecosystem.config.js`(或 `.json`),產出檔案可被 `pm2 start` 直接載入,**不需要改動** `config` 套件、不需要 daemon 互動、不需要新外部相依(僅升級一個已是 indirect 的 `mattn/go-isatty` 為 direct)。

## Design summary

- 新增 `cmd/eco.go` — cobra 指令 + 互動 prompt + JS/JSON 渲染器
- 新增 `cmd/eco_test.go` — stdlib `testing`,覆蓋渲染器與 round-trip
- 編輯 `cmd/root.go` — 在 `init()` 註冊 `newEcoCmd()`
- 編輯 `go.mod` — 把 `mattn/go-isatty` 由 indirect 升為 direct
- **不動** `config/ecosystem.go` — `AppConfig` 已有所有需要的欄位

## File-level changes

### 1. `cmd/eco.go` (新檔)

#### Pure functions (testable, no I/O side effects)

```go
// Render canonical PM2 JS form. 4-space indent, double quotes.
// Skips zero-value fields (args, env, watch, cron, cron_restart) to
// match README example at README.md:220-232.
func renderEcosystemJS(apps []config.AppConfig) string

// JSON counterpart for --format json. Uses encoding/json with indent.
func renderEcosystemJSON(apps []config.AppConfig) (string, error)

// Reads a single line from in, trims, returns. Empty == def.
func promptLine(in io.Reader, out io.Writer, label, def string) (string, error)

// y/N with explicit default. Accepts y/yes/n/no (case-insensitive).
func promptYesNo(in io.Reader, out io.Writer, label string, def bool) (bool, error)

// Walks the per-app question block, loops on "add another app?".
// EOF on stdin returns an error.
func collectAnswers(in io.Reader, out io.Writer) ([]config.AppConfig, error)
```

#### Per-app question block (≤ 8 questions, in this order)

Empty input == shown default. Per question:

1. `Script path? [default: app.js]` — required
2. `Process name? [default: <derived from script>]`
3. `Args? (space-separated, optional)`
4. `Namespace? [default: default]`
5. `Instances? [default: 1]` — int parse; bad input re-prompt 3×, fall back to 1
6. `Watch mode? [y/N]`
7. `Env vars? (one per line KEY=VAL, blank line to finish)` — loop
8. `Cron schedule? (e.g. "*/5 * * * *", blank to skip)` — if non-blank also ask `Cron restart? [y/N]`

Then if not the last app:
```
  -> app #1: name=api script=./server.js instances=1 namespace=default watch=false cron=""
Add another app? [y/N]
```

Hard cap: 64 apps (prevents runaway loops in scripted misuse).

#### JS output template (matches README:220-232)

```js
module.exports = {
    apps: [
        {
            // api (default)
            name: "api",
            script: "./bin/server",
            args: ["--port", "8080"],
            namespace: "default",
            instances: 2,
            watch: true,
            env: {
                "NODE_ENV": "production",
            },
            cron_restart: "0 3 * * *",
            cron: "0 * * * *",
        },
    ],
};
```

Rules:
- `Args` / `Env` / `Watch` / `Cron` / `CronRestart` omitted when zero.
- String escaping: `strconv.Quote` (Go's own JSON-style escapes round-trip safely through goja).
- `Instances` 0 → emit as `1` (matches `Normalize()` behavior at `config/ecosystem.go:41-43`).

#### `newEcoCmd()` factory

```go
func newEcoCmd() *cobra.Command
```

Flags:
- `-o, --output` string, default `""` → resolved to `ecosystem.config.js` (or `.json` if `--format json`)
- `-f, --force` bool, overwrite existing
- `--format` string, `js` (default) | `json`
- `-y, --yes` bool, non-interactive, all defaults, single app with `script="app.js"`

`RunE` flow:
1. Validate `--format` ∈ {`js`,`json`}.
2. Default `output` by format if empty.
3. **TTY gate**: `isatty.IsTerminal(os.Stdin.Fd())`. If false and `--yes` not set, print clear error and return non-zero.
4. If `--yes`: skip prompts, use `defaultApp()`.
5. Else: `collectAnswers(cmd.InOrStdin(), cmd.OutOrStdout())`.
6. Render to `[]byte`.
7. Print preview to `cmd.ErrOrStderr()` (keeps stdout clean).
8. Confirm `Write <path>? [Y/n]` (skipped under `--yes`).
9. Refuse overwrite without `--force`.
10. `os.WriteFile(output, data, 0o644)`, print `Wrote <abs-path>` to stdout.

### 2. `cmd/eco_test.go` (新檔)

Style matches `config/ecosystem_test.go`: stdlib `testing` only, flat `// Test N:` comments, `os.MkdirTemp("", "pm2-test")` + `defer os.RemoveAll`, `t.Errorf("Expected %q, got %q", ...)`.

Tests:
1. `TestRenderEcosystemJSEmpty` — `renderEcosystemJS(nil)` parses via `config.Load` (write to temp file, load, assert zero apps).
2. `TestRenderEcosystemJSSingle` — golden-string match against the literal template above.
3. `TestRenderEcosystemJSSkipsEmpty` — `Args=[]`, `Env=nil`, `Watch=false`, `Cron=""` → those keys absent in output.
4. `TestRenderEcosystemJSON` — output unmarshals back to identical `EcosystemConfig`.
5. `TestRenderRoundTrip` — for 1-app, 2-app, env+args+cron variants: `renderEcosystemJS → write → config.Load → cfg.Apps deep-equal input after Normalize`.
6. `TestCollectAnswersSingle` — feed `strings.Reader` with the prompt sequence; assert one app with expected defaults.
7. `TestCollectAnswersMulti` — same + "y\n" + second app + "n\n".
8. `TestPromptYesNoDefaults` — empty input returns the default.
9. `TestRenderEscapes` — `Script = "weird\"name\\path"` round-trips intact.

### 3. `cmd/root.go` (編輯)

In `init()` (lines 41-52), add `newEcoCmd()` to the `rootCmd.AddCommand(...)` argument list. One-line change. No other edits to that file.

### 4. `go.mod` (編輯)

Promote `mattn/go-isatty v0.0.20` from indirect (currently at `go.mod:30`) to direct. `go mod tidy` afterwards. Already pulled by bubbletea — no new download. Required for the TTY gate in step 3.6 of `RunE`.

## Reused existing utilities

- `config.AppConfig` — schema is already complete; no fields added (`config/ecosystem.go:15-32`)
- `config.EcosystemConfig{Apps []AppConfig}` — exact top-level shape we emit (`config/ecosystem.go:35-37`)
- `config.Load(path)` — accepts both `.js` and `.json` (`config/ecosystem.go:87-101`); used by round-trip tests and by `pm2 start` after wizard exits
- `os.UserHomeDir` / `os.Getwd` — `cmd/root.go:33-39` already uses home; we use `os.Getwd` for relative-path default
- cobra flag pattern `cmd.Flags().StringVarP(&v, "name", "n", "", "desc")` — see `cmd/start.go:140-148`
- Factory pattern `new<Name>Cmd() *cobra.Command` — see `cmd/monitor.go:13`, `cmd/start.go:16`, `cmd/daemon.go:16`

## Verification

After implementation, in `/Users/bytedance/projects/pm2`:

1. `go build ./...` — must succeed.
2. `go vet ./...` — must be clean.
3. `go test ./...` — all green, including new `cmd/eco_test.go`.
4. **Manual happy path**:
   ```bash
   cd /tmp && mkdir eco-test && cd eco-test && touch app.js
   pm2 eco   # walk through prompts
   cat ecosystem.config.js   # visually verify template
   ```
5. **Manual error paths**:
   - `pm2 eco` in a dir with existing `ecosystem.config.js` and no `-f` → refuse.
   - `pm2 eco | cat` (non-TTY) without `--yes` → clear error, non-zero exit.
   - `pm2 eco --yes` → writes default single-app config without prompting.

Wizard's success criterion is producing a valid config file; the daemon does not need to be running. The round-trip test (#5 in `cmd/eco_test.go`) proves the file is loadable by `config.Load`.

## Out of scope

- No TUI for the wizard — plain `bufio.Scanner` line prompts only. `monit` keeps the bubbletea TUI.
- No editing of existing files — `pm2 eco` is generate-only.
- No validation of `args` tokenisation, cron expressions, env var names — daemon/shell are source of truth.
- No support for fields outside `AppConfig` (e.g. `deploy`).
