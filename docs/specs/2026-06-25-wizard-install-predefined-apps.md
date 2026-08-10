# pm2 wizard install — pre-defined app installer

## Context

The `pm2 wizard` command interactively builds an ecosystem file. The new
`pm2 wizard install <script> [user_prompt]` subcommand provides a
zero-prompt shortcut to register a pre-configured process — useful for
bootstrapping tools like `agy` or `claude` whose only "config" is a
prompt payload. It writes a single AppConfig into `ecosystem.config.js`
(via the existing merge-on-exists flow) and `pm2 start`s it.

The two prompt flags carry a pm2-internal prefix that the wrapped
process consumes (e.g. `agy -p "..."`). `user_prompt` is appended (or
left empty if the user passes none).

## Decisions (confirmed with user)

| # | Decision | Why |
|---|----------|-----|
| 1 | `args` is always the 3-element array `["-p", <prefix>, <user_prompt>]`. `<user_prompt>` is the empty string when omitted. | User explicitly asked for this shape; allows downstream process to index `args[1]`/`args[2]` without checking length. |
| 2 | `--system-planner` and `--business-planner` are mutually exclusive; exactly one must be supplied. They map to fixed pm2-side prefix strings. | User-provided mapping. Mutex + required prevents ambiguous output. |
| 3 | Subcommand: positional `pm2 wizard install <script> [user_prompt]`. | User picked positional over all-flags. Keeps CLI surface minimal. |
| 4 | Reuse the merge-on-exists flow (`loadExistingApps` + `mergeAppsByName` + `runWizardEndToEnd` style). | User confirmed; the install command is "wizard with pre-filled answers." |
| 5 | `script` must exist on disk; abort if not (unlike the interactive wizard which only warns). | Install is "ready to run" — a missing script would just produce a daemon error. Fail fast. |
| 6 | `wizard install` is implemented as a cobra subcommand of the existing `wizard` command, not a sibling of it. | The install logic is wizard-specific (it produces an AppConfig from pre-filled answers). |

## Prompt prefix table

| Flag | `args[1]` value |
|------|-----------------|
| `--system-planner`   | `[plan only] run /system-planner and output to ./plans/` |
| `--business-planner` | `[plan only] run /business-planner and output to ./plans/` |

## Critical files

- `cmd/eco.go` — refactor: turn `wizard` from a single-run command into a
  parent with subcommands. Add `newEcoInstallCmd()`. Pull the AppConfig
  build + merge-write into a shared helper so the existing
  `newEcoCmd().RunE` and the new `install` subcommand can both call it.
- `cmd/eco_test.go` — add tests for: (a) prompt prefix table, (b) the
  args array shape, (c) mutual-exclusion of flags, (d) end-to-end
  install via cobra (`install agy /abs/path/script.js --system-planner "..."`).
- `cmd/root.go` — no change. `newEcoCmd()` is already registered.
- `config/ecosystem.go` — no change.
- `daemon/*` — no change. We reuse `daemon.SendRequest` exactly as
  `start.go` does.

## Implementation

### Restructure `newEcoCmd`

Change `newEcoCmd` so that the existing interactive flow is a
subcommand (`wizard interactive`, alias `wizard i`, default run when no
subcommand is given — keep `wizard` itself working as today by
preserving its current `RunE` on the parent as a passthrough that
delegates to `wizard interactive`).

Concretely:

1. `newEcoCmd()` returns a parent cobra command with `Use: "wizard"`.
2. Add `newEcoInteractiveCmd()` carrying the existing logic (the
   current `RunE` body, verbatim).
3. Add `newEcoInstallCmd()` (new).
4. Parent attaches both. `interactive` is the default (set
   `RunE` on the parent to delegate when no subcommand matched — or
   set `interactive` as the default subcommand via `SetHelpFunc` /
   `RunE` dispatch; the simplest is to add a `RunE` on the parent that
   errors out asking the user to pick a subcommand, AND mark
   `interactive` as the default by also calling the parent's logic
   when the user types just `wizard`).

**Default-run shim**: keep `pm2 wizard` (no subcommand) behaving as
the interactive flow today. Implemented by giving the parent `RunE`
the same body as `interactive` and treating `interactive` as a hidden
alias `pm2 wizard interactive` for completeness.

### New `newEcoInstallCmd()`

```go
type installFlags struct {
    systemPlanner   bool
    businessPlanner bool
    output          string  // default ecosystem.config.js
    force           bool
    noMerge         bool
}

const (
    ecoPlannerSystem   = "[plan only] run /system-planner and output to ./plans/"
    ecoPlannerBusiness = "[plan only] run /business-planner and output to ./plans/"
)
```

`RunE`:

1. `cobra.ExactArgs(2)` (or `RangeArgs(1,2)` if the user wants to
   allow omitting the user prompt at the CLI level — keep it 2,
   defaulting `user_prompt` to `""` if empty).
2. Validate flags: exactly one of `--system-planner` /
   `--business-planner` is set; abort with clear error otherwise.
3. `script := args[0]`; `userPrompt := args[1]`.
4. `if _, err := os.Stat(script); err != nil { return ... }` —
   hard fail, no warning.
5. Build `prefix` from the chosen flag.
6. Build `app := config.AppConfig{
       Name:      deriveName(script),
       Script:    script,
       Args:      []string{"-p", prefix, userPrompt},
       Instances: 1,
   }`
   `app.Normalize()`.
7. Call the **shared helper** `writeAndStart([]config.AppConfig{app}, ...)`
   described below.

### Shared helper — `writeAndStart(apps []config.AppConfig, output string, force, noMerge bool) ([]string, error)`

Lifts the merge-or-replace-then-write-then-RPC block out of
`newEcoInteractiveCmd.RunE`. Returns the names of the apps that were
started (or `[]string{}` if the user aborted the confirm prompt).

```text
1. existing exists? yes → load + merge  | no → just apps
2. format from --output ext (or --format when no file)
3. render bytes
4. preview + summary
5. if !yesAll: confirm prompt; abort returns ("", nil)
6. WriteFile
7. for each app: build daemon.Request{CmdStart, AppStartReq{...}}
   and daemon.SendRequest(socketPath(), req)
8. return started names
```

`newEcoInstallCmd` calls this with `yesAll = true` (install is
non-interactive by definition; the user already typed the prompt on
the CLI).

### Flag surface on the parent wizard

Unchanged for the interactive case. The new subcommand owns its own
flags:

```text
--system-planner       use the system-planner prefix (mutex with --business-planner)
--business-planner     use the business-planner prefix (mutex with --system-planner)
-o, --output PATH     ecosystem file to write (default ./ecosystem.config.js)
-f, --force            replace existing file instead of merging
--no-merge             abort if file exists (legacy)
```

## Tests (add to `cmd/eco_test.go`)

Unit tests:

- `TestPlannerPrefixes` — table-driven over the two flag values,
  asserts the literal prefix strings used in `args[1]`.
- `TestBuildInstallApp` — given a script path and a chosen planner,
  asserts `Args == []string{"-p", <prefix>, <userPrompt>}` and
  `Script == script` and `Instances == 1`.
- `TestBuildInstallAppEmptyUserPrompt` — `userPrompt == ""` still
  produces a 3-element `Args` with the last element as `""`.
- `TestInstallFlagMutex` — invoking the cobra command with both
  `--system-planner` and `--business-planner` returns a non-nil error
  whose message mentions "mutually exclusive".
- `TestInstallMissingScript` — `install nope --system-planner "x"`
  returns a non-nil error.

End-to-end:

- `TestInstallEndToEnd` — pre-seed `ecosystem.config.js` with one app,
  run `install` with a different script name + `--system-planner "do X"`,
  parse the resulting file with `config.Load`, assert:
  - both apps present
  - new app's `Args[0] == "-p"`, `Args[1] == planner prefix`, `Args[2] == "do X"`
- `TestInstallNoUserPrompt` — same as above with `args[1] == ""` and
  verify `args[2] == ""`.

Daemon RPC is not exercised in tests; we verify the **file contents**
and trust the existing `daemon.SendRequest` path (covered by `start.go`
end-to-end tests already in the suite).

## Verification

```bash
go build ./...
go test ./cmd/... -v
go test ./...
go vet ./...
```

Manual smoke:

```bash
mkdir /tmp/install-smoke && cd /tmp/install-smoke
cat > dummy.js <<'EOF'
console.log("hello from agy");
EOF
go -C /Users/bytedance/projects/pm2 run . wizard install ./dummy.js "analyze repo" --system-planner
cat ecosystem.config.js
go -C /Users/bytedance/projects/pm2 run . list
go -C /Users/bytedance/projects/pm2 run . delete agy
```

## Out of scope

- No changes to `daemon.Server` or `daemon.Request` types.
- No new top-level cobra command under root.
- No new env-var passthrough; everything goes in `args`.
- No dynamic prefix registration — the two prefixes are hard-coded.
