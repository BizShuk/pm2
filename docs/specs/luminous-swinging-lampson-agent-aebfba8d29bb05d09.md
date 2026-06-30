# Plan: Add `pm2 eco` / `pm2 init` interactive wizard

Read-only investigation. No source files will be modified in this plan.

## 1. Existing schema & defaults

File: `/Users/bytedance/projects/pm2/config/ecosystem.go` (lines 15-32)

`AppConfig` struct (all JSON tags, all lower-snake_case):

| Field          | JSON tag        | Default from `Normalize()` (lines 40-84) | User-overridable? |
| -------------- | --------------- | ---------------------------------------- | ----------------- |
| `Namespace`    | `namespace`     | `"default"`                              | yes |
| `Name`         | `name`          | `filepath.Base(Script)` minus ext        | yes |
| `Script`       | `script`        | **REQUIRED** (no default; `Normalize` does not fill it) | yes (required) |
| `Args`         | `args`          | `nil` (zero-value slice)                 | yes |
| `Instances`    | `instances`     | `1`                                      | yes |
| `Env`          | `env`           | `nil` (empty map)                        | yes |
| `CronRestart`  | `cron_restart`  | `""`                                     | yes |
| `Cron`         | `cron`          | `""`                                     | yes |
| `Watch`        | `watch`         | `false`                                  | yes |
| `MaxRestarts`  | `max_restarts`  | `15`                                     | yes |
| `Version`      | `version`       | `"-"`                                    | yes |
| `LogFile`      | `log_file`      | derives from `ConfigDir` → `logs/daemon.log`, or `OutFile` if set | yes |
| `OutFile`      | `out_file`      | `""`                                     | yes |
| `ErrorFile`    | `error_file`    | derives from `ConfigDir` → `logs/daemon.err` | yes |
| `ConfigDir`    | `config_dir`    | derived: dir of `OutFile`/`LogFile`/`ErrorFile`, else `~/.config/<Name>` | yes |
| `ConfigFile`   | `config_file`   | auto-set to abs config path by `Load()`  | rarely (auto) |

`EcosystemConfig` (line 35-37) wraps `Apps []AppConfig` with `json:"apps"`.

Notes:
- The doc-comment at line 18 marks `Script` as **Required**. `Normalize()` never fills it.
- `Name` is only auto-derived if `Script` is non-empty (line 50-53).
- `ConfigFile` is always rewritten by `Load()` to the absolute path of the loaded file (line 116, 156) — the wizard should not write this; `Load()` will set it.
- `Normalize()` calls `filepath.Base` so the same field semantics are preserved for JS configs.

## 2. Sample ecosystem files

### README canonical examples
File: `/Users/bytedance/projects/pm2/README.md` lines 193-232

- JSON form: `{"apps": [{ ... }]}`
- JS form: `module.exports = { apps: [ { ... } ] };` (no semicolons required, semicolons allowed).

```js
module.exports = {
    apps: [
        {
            name: "api",
            script: "./bin/server",
            instances: 2,
            env: { NODE_ENV: "production", PORT: "8080" },
            cron_restart: "0 3 * * *"
        }
    ]
};
```

### In-repo examples
- `/Users/bytedance/projects/env_setup/ecosystem.config.js` — uses `namespace`, `name`, `script: "go"`, `args: ["clean","-cache"]`, `cron`, `autorestart` (note: `autorestart` is NOT a field in our `AppConfig`; it will be ignored silently by `json.Unmarshal`).
- `/Users/bytedance/projects/cc-plugin/ecosystem.config.js` — uses `namespace`, `name`, `script: "agentmemory"`, `config_dir: "~/.config/agentmemory"`.

Both are 4-space indented, use double-quoted strings, follow the `module.exports = { apps: [...] }` convention. No `//` or `/* */` comments in either.

### Inside pm2 itself
- No `ecosystem.config.js` lives in the pm2 repo (only the `plans/*.md` files mention them).
- `cmd/start.go` line 35 hard-codes the default target name `"ecosystem.config.js"` — same string the wizard should write.

## 3. Existing prompt patterns

File: `/Users/bytedance/projects/pm2/go.mod` and grep results.

- `bufio` is used for file/stream reading only: `cmd/logs.go:4` (scanner over log files), `tui/model.go:4` (scanner over log file in TUI). Neither reads from `os.Stdin`.
- `fmt.Scan*` — not used anywhere (`cmd/logs.go:44` uses `fmt.Sscan` for parsing an int ID, not interactive input).
- `os.Stdin` — not referenced anywhere.
- `survey`, `promptui`, `ahocorasick` — not in `go.mod`, not in code.
- `golang.org/x/term` — listed as an *indirect* dep (line 32 of go.mod) via bubbletea. Not directly used.
- `mattn/go-isatty` — listed as indirect dep (line 30 of go.mod), pulled in by bubbletea. Not used directly.
- TUI interaction uses `github.com/charmbracelet/bubbletea` (top-level dep) and `lipgloss` — overkill for a CLI wizard but already vetted by the project.

`os.UserHomeDir` is used in:
- `cmd/root.go:33` (sets `pm2Home = ~/ .pm2`)
- `daemon/server.go:804`

No interactive prompt library precedent. Recommendation: stdlib `bufio.NewReader(os.Stdin)` + `fmt.Print` is consistent with project style. Bubble Tea is available if richer UX is wanted but is currently used only for the `monit` dashboard.

## 4. Cmd registration pattern

File: `/Users/bytedance/projects/pm2/cmd/root.go` lines 14-58.

Construction (lines 14-17):
```go
var rootCmd = &cobra.Command{
    Use:   "pm2",
    Short: "PM2-like process manager written in Go",
}
```

Subcommand registration (lines 41-52) uses `rootCmd.AddCommand(newStartCmd(), newStopCmd(), ...)`. Each builder returns `*cobra.Command` and is named `new<Name>Cmd()` (e.g. `newMonitCmd()` in `cmd/monitor.go:13`, `newLogsCmd()` in `cmd/logs.go:16`, `newDaemonCmd()` in `cmd/daemon.go:16`).

Standard cobra fields used across the codebase:
- `Use` — short name, sometimes with `[args]` placeholder (e.g. `cmd/start.go:28`).
- `Short` — one-line description (all files).
- `Long` — NOT used by any existing command.
- `Aliases` — used in `cmd/monitor.go:18` (`[]string{"m", "monitor", "dashboard"}`) and `cmd/stop.go:57` (`[]string{"del"}`).
- `Args` — uses `cobra.ArbitraryArgs` (`cmd/start.go:30`), `cobra.MaximumNArgs(1)` (`cmd/logs.go:21`), `cobra.ExactArgs(1)` (`cmd/stop.go:14`). No `cobra.NoArgs` usage found.
- `RunE` — return `error`; used everywhere. `Run` is not used.
- `Hidden: true` is set on `daemon` (`cmd/daemon.go:21`).

Global init (root.go lines 54-57):
```go
cobra.EnableTraverseRunHooks = true
metric.CobraCMDHook(rootCmd)
```

The metric import path is `github.com/bizshuk/gosdk/metric`. The hook must be applied to `rootCmd` (not per-subcommand) and depends on `EnableTraverseRunHooks = true`.

## 5. Output path

- `cmd/start.go:34-35` — when no args given, target is hard-coded to `"ecosystem.config.js"` (relative path, resolved via `filepath.Abs` later in `config.Load` at `config/ecosystem.go:88-91`).
- `cmd/root.go:33-39` — `os.UserHomeDir()` is the precedent for getting the home dir (used for `pm2Home` not for user output paths).
- No command in the repo currently exposes an `--output` / `-o` flag. Adding it to the new wizard would be novel but not out of style — flags are declared with `cmd.Flags().StringVarP(&v, "name", "n", "", "desc")` (see `cmd/start.go:140-148` for the canonical pattern: `StringVarP`, `IntVarP`, `StringVar`, `StringArrayVarP`, `BoolVarP`).
- `cmd/start.go:28` shows the convention: when defaulting, use the literal string `"ecosystem.config.js"`. The wizard should default to `cwd/ecosystem.config.js` (via `os.Getwd()`) and offer `--output` / `-o` override.

## 6. Tests

Files:
- `/Users/bytedance/projects/pm2/config/ecosystem_test.go`
- `/Users/bytedance/projects/pm2/daemon/server_test.go`
- `/Users/bytedance/projects/pm2/tui/model_test.go`

Style observations (all three files):
- Use stdlib `testing` only — no testify, gomock, etc.
- `t.Fatalf`, `t.Errorf` for assertions.
- `os.MkdirTemp("", "pm2-test")` is used in `config/ecosystem_test.go:11` for tmp dirs (NOT `t.TempDir()` even though Go 1.15+ has it). Follow this pattern.
- Sequential numbered comments + ad-hoc `Test 1: ...`, `Test 2: ...` style (see `config/ecosystem_test.go:17-63`). No `t.Run` subtests, no table-driven tests. `daemon/server_test.go:18` follows the same flat-style.
- Direct field comparison with `t.Errorf("Expected %q, got %q", ...)`.

Recommendation for wizard test: write the wizard's file-generation function as a pure function that takes a struct of answers + a path, then test it by writing into `os.MkdirTemp` and re-loading via `config.Load()` to confirm round-trip equality. This avoids needing to drive `os.Stdin` in tests.

## 7. Terminfo / TTY detection

Grep results:
- `os.Stdin` — no occurrences in the repo.
- `isatty` / `term.IsTerminal` — no occurrences in the repo.
- `golang.org/x/term` — only as an indirect dep via bubbletea, never imported directly.
- `--yes` / `-y` flag — no occurrences.
- `cobra.NoArgs` — no occurrences (commands use `ArbitraryArgs`, `MaximumNArgs(1)`, `ExactArgs(1)`).
- The only interactive surfaces today are the bubbletea TUI in `monit` and the daemon's Unix-socket RPC.

There is no precedent for non-TTY fallback or a "skip prompts" flag. The wizard will be a greenfield design.

## Summary of file paths touched by the plan (read-only references)

- `/Users/bytedance/projects/pm2/config/ecosystem.go`
- `/Users/bytedance/projects/pm2/cmd/root.go`
- `/Users/bytedance/projects/pm2/cmd/start.go`
- `/Users/bytedance/projects/pm2/cmd/monitor.go`
- `/Users/bytedance/projects/pm2/cmd/logs.go`
- `/Users/bytedance/projects/pm2/cmd/daemon.go`
- `/Users/bytedance/projects/pm2/cmd/stop.go`
- `/Users/bytedance/projects/pm2/config/ecosystem_test.go`
- `/Users/bytedance/projects/pm2/daemon/server_test.go`
- `/Users/bytedance/projects/pm2/tui/model_test.go`
- `/Users/bytedance/projects/pm2/README.md`
- `/Users/bytedance/projects/pm2/CLAUDE.md`
- `/Users/bytedance/projects/pm2/go.mod`
- `/Users/bytedance/projects/env_setup/ecosystem.config.js` (external sample)
- `/Users/bytedance/projects/cc-plugin/ecosystem.config.js` (external sample)
