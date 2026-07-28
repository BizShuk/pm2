# PM2 Command Namespaces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Separate daemon startup from task execution, keep the explicitly requested `pm2 start` daemon alias, and expose `pm2 apply` as the documented short alias for `pm2 task start`, including an interactive `--single` app selector.

**Architecture:** Keep every Cobra command as a package-level singleton, as required by `CLAUDE.md`. The `pm2 task` namespace and all task handlers live in the `cmd/task` subpackage. Register a command at the root only when the requirements explicitly name it as an alias. Keep selection pure in `select.go`, interactive input in `single.go`, and Cobra wiring in `apply.go` / `start.go`.

**Tech Stack:** Go 1.26.3, Cobra, standard `testing` package.

## Global Constraints

- Keep one file focused on one command responsibility.
- Do not introduce `NewXxxCmd` command constructors.
- Keep `pm2 daemon start` working and make top-level `pm2 start` behaviorally identical.
- Use `pm2 task start` for ecosystem files and remote repositories.
- Register `pm2 apply` as the explicit short alias for `pm2 task start`.
- `pm2 apply` defaults to `./ecosystem.config.js`.
- `--single` must choose and apply exactly one app from the loaded ecosystem file.
- `--single` is incompatible with `--all` and `--with`.
- Keep restart canonical at `pm2 task restart`; do not register `pm2 restart`.
- Do not register other implicit root aliases for task lifecycle commands.
- Do not rewrite historical documents under `docs/specs/`.
- Do not commit changes unless the user asks.

---

### Task 1: Initial command-tree regression tests

**Files:**

- Modify: `main_test.go`

**Interfaces:**

- Consumes: the exported `RootCmd` command tree.
- Produces: regression coverage for daemon `start`, the explicit `apply` alias, canonical `task` command paths, and the absence of other implicit root aliases.

- [x] **Step 1: Write the failing command-tree tests**

Add tests that assert:

```go
RootCmd.Find([]string{"start"})
RootCmd.Find([]string{"daemon", "start"})
RootCmd.Find([]string{"task", "start"})
RootCmd.Find([]string{"task", "stop"})
RootCmd.Find([]string{"task", "pause"})
RootCmd.Find([]string{"task", "resume"})
RootCmd.Find([]string{"task", "delete"})
```

The tests compare the explicitly requested daemon-start alias handlers, require
all task children to use `RunE`, and assert that `run`, `stop`, `pause`,
`resume`, and `delete` are absent from the root.

- [x] **Step 2: Run the tests and verify RED**

Run:

```bash
go test ./... -run 'TestRootCmd(CommandNamespaces|TaskSubcommands)$' -count=1
```

Expected: failure while `apply` is missing or the legacy `run` alias remains registered at the root.

### Task 2: Implement the initial command namespaces

**Files:**

- Move task execution from: `cmd/start.go` to `cmd/task/start.go`
- Create: `cmd/start.go`
- Create: `cmd/task/task.go`
- Create: `cmd/task/apply.go`
- Move: `cmd/start_select.go` to `cmd/task/select.go`
- Move: `cmd/start_select_test.go` to `cmd/task/select_test.go`
- Move: `cmd/stop.go` to `cmd/task/stop.go`
- Move: `cmd/pause.go` to `cmd/task/pause.go`
- Move: `cmd/resume.go` to `cmd/task/resume.go`
- Move: `cmd/delete.go` to `cmd/task/delete.go`
- Modify: `cmd/daemon_start.go`
- Modify: `main.go`

**Interfaces:**

- Consumes: existing RPC request construction and daemon start helpers.
- Produces: `appcmd.StartCmd`, `taskcmd.Cmd`, `taskcmd.ApplyCmd`, task-owned handlers, and the new root command tree.

- [x] **Step 1: Move ecosystem execution under task**

Move ecosystem execution to `task.StartCmd` with:

```go
"start [ecosystem.config.js|ecosystem.config.json|owner/repo|https://github.com/...]"
```

Keep its handler in the same subpackage:

```go
func runTasks(cmd *cobra.Command, args []string) error
```

- [x] **Step 2: Add the daemon start short command**

Create a top-level `StartCmd` whose `RunE` is the same `runDaemonStart` function used by `DaemonStartCmd`. Bind `-f, --foreground` independently on both command nodes.

- [x] **Step 3: Move task lifecycle commands**

Create `task.Cmd` with `start`, `restart`, `stop`, `pause`, `resume`, and
`delete` children. Each child owns its handler:

```text
pm2 task start
pm2 task restart
pm2 task stop
pm2 task pause
pm2 task resume
pm2 task delete
```

Register and advertise only `pm2 apply` as the explicit short alias for
`pm2 task start`; do not create aliases for the other task commands.

- [x] **Step 4: Register the new roots**

Register daemon `appcmd.StartCmd`, `taskcmd.ApplyCmd`, and `taskcmd.Cmd` in `main.go`.

- [x] **Step 5: Run the focused tests and verify GREEN**

Run:

```bash
go test ./... -run 'TestRootCmd(CommandNamespaces|TaskSubcommands)$' -count=1
```

Expected: PASS.

### Task 3: Synchronize the initial active documentation and user-facing hints

**Files:**

- Modify: `README.md`
- Modify: `CLAUDE.md`
- Modify: `docs/usage.md`
- Modify: `skills/pm2/SKILL.md`
- Modify: `process/types.go`
- Modify: `model/protocol_test.go`
- Modify: `cmd/eco.go`
- Modify: `cmd/eco_install.go`
- Modify: `config/wizard/wizard.go`
- Modify: `tui/views/layout.go`
- Modify: `tui/views/list.go`

**Interfaces:**

- Consumes: the implemented command tree.
- Produces: one current command vocabulary across help, docs, generated next steps, wizard prompts, and empty-state hints.

- [x] **Step 1: Replace active ecosystem execution examples**

Use `pm2 task start ecosystem.config.js`; reserve `pm2 start` for daemon startup.

- [x] **Step 2: Document explicit aliases**

Document the explicit aliases:

```text
pm2 start              == pm2 daemon start
pm2 apply <target>     == pm2 task start <target>
```

Other task lifecycle commands remain canonical under `pm2 task` with no root aliases.

- [x] **Step 3: Scan active sources**

Run:

```bash
rg -n 'pm2 start .*ecosystem|pm2 start <script|`pm2 start` applies' README.md CLAUDE.md docs/usage.md skills/pm2 cmd config process model tui
```

Expected: no stale active reference that uses `pm2 start` to run tasks.

### Task 4: Verify the initial namespace change

**Files:**

- Verify all modified Go and Markdown files.

**Interfaces:**

- Consumes: all changes from Tasks 1-3.
- Produces: fresh verification evidence.

- [x] **Step 1: Format Go files**

Run:

```bash
gofmt -w main.go main_test.go cmd/*.go cmd/task/*.go
```

- [x] **Step 2: Run focused and full tests**

Run:

```bash
go test ./... -run 'TestRootCmd(CommandNamespaces|TaskSubcommands)$' -count=1
go test ./... -count=1
go test -race ./... -count=1
```

Expected: all packages pass.

- [x] **Step 3: Build and inspect help**

Run:

```bash
go build ./...
go run . --help
go run . task --help
go run . start --help
```

Expected: build succeeds, help shows the intended namespaces and the `apply`
short alias, and root help does not list `stop`, `pause`, `resume`, or
`delete`.

- [x] **Step 4: Review the working tree**

Run:

```bash
git diff --check
git status --short
git diff --stat
```

Expected: no whitespace errors and only intended files are modified.

### Task 5: Replace `run` with `apply` and add single-app selection

**Files:**

- Modify: `main_test.go`
- Modify: `cmd/task/select_test.go`
- Create: `cmd/task/start_test.go`
- Delete: `cmd/task/run.go`
- Create: `cmd/task/apply.go`
- Create: `cmd/task/single.go`
- Modify: `cmd/task/start.go`
- Modify: `cmd/task/select.go`
- Modify: `main.go`

**Interfaces:**

- Consumes: `config.Load`, `process.AppConfig`, `runTasks`, Cobra command I/O.
- Produces: `taskcmd.ApplyCmd` and `chooseSingleApp(apps, in, out)`.

- [x] **Step 1: Write failing command-tree tests**

Assert that root `apply` exists, root `run` does not, `apply` shares the
`task start` handler, and both command nodes expose `--single`.

- [x] **Step 2: Verify command-tree RED**

Run:

```bash
go test . -run 'TestRootCmd(CommandNamespaces|TaskSubcommands)$|TestTaskCommandsLiveInSubpackage$' -count=1
```

Expected: FAIL because `apply` and `cmd/task/apply.go` do not exist.

- [x] **Step 3: Write failing selector tests**

Cover numeric choice, namespaced-name choice, invalid input, empty ecosystem,
and the invariant that the selected optional app is active and is the only app
returned.

- [x] **Step 4: Verify selector RED**

Run:

```bash
go test ./cmd/task -run 'TestChooseSingleApp|TestSelectSingleApp' -count=1
```

Expected: FAIL because the single-app selector does not exist.

- [x] **Step 5: Implement the minimal behavior**

Replace `RunCmd` with `ApplyCmd`, bind `--single` on both start entry points,
reject conflicting selection flags, prompt through Cobra's configured input
and output streams, and send only the chosen app to the daemon.

- [x] **Step 6: Verify GREEN**

Run:

```bash
go test . ./cmd/task -run 'TestRootCmd|TestChooseSingleApp|TestSelectSingleApp' -count=1
```

Expected: PASS.

### Task 6: Synchronize and verify the `apply` interface

**Files:**

- Modify: `README.md`
- Modify: `CLAUDE.md`
- Modify: `docs/usage.md`
- Modify: `skills/pm2/SKILL.md`
- Modify: `plans/2026-07-28-command-namespaces.md`

**Interfaces:**

- Consumes: the verified command tree and selector behavior.
- Produces: one current command vocabulary across help and active docs.

- [x] **Step 1: Refresh drifted docs**

Replace prior `pm2 run` alias claims with `pm2 apply`, document that plain
`apply` loads `./ecosystem.config.js`, and document the interactive
`--single` flow.

- [x] **Step 2: Run fresh verification**

Run:

```bash
gofmt -w main.go main_test.go cmd/task/*.go
go test ./... -count=1
go test -race ./... -count=1
go build ./...
go run . --help
go run . apply --help
go run . task start --help
git diff --check
```

Expected: all commands exit successfully; help lists `apply`, not `run`, and
documents `--single`.

### Task 7: Move restart under the task namespace

**Files:**

- Modify: `main_test.go`
- Delete: `cmd/restart.go`
- Create: `cmd/task/restart.go`
- Modify: `cmd/task/task.go`
- Modify: `main.go`

**Interfaces:**

- Consumes: `model.CmdRestart` and the shared CLI socket path.
- Produces: `taskcmd.RestartCmd` at `pm2 task restart <name|id|all>`.

- [x] **Step 1: Write failing command-tree and structure tests**

Assert that `pm2 task restart` exists and uses `RunE`, root `pm2 restart` is
absent, `cmd/task/restart.go` exists, and `cmd/restart.go` is absent.

- [x] **Step 2: Verify RED**

Run:

```bash
go test . -run 'TestRootCmd(CommandNamespaces|TaskSubcommands)$|TestTaskCommandsLiveInSubpackage$' -count=1
```

Expected: FAIL because restart is still registered at the root and is not a
child of `pm2 task`.

- [x] **Step 3: Move the command handler**

Move the existing Cobra singleton into package `task`, use the shared exported
socket path from package `cmd`, add it to `task.Cmd`, and remove the root
registration from `main.go`.

- [x] **Step 4: Verify GREEN**

Run:

```bash
go test . -run 'TestRootCmd(CommandNamespaces|TaskSubcommands)$|TestTaskCommandsLiveInSubpackage$' -count=1
```

Expected: PASS.

### Task 8: Refresh and verify restart usage

**Files:**

- Modify: `README.md`
- Modify: `CLAUDE.md`
- Modify: `docs/usage.md`
- Modify: `skills/pm2/SKILL.md`
- Modify: `plans/2026-07-28-command-namespaces.md`

**Interfaces:**

- Consumes: the verified nested command tree.
- Produces: active docs that use only `pm2 task restart`.

- [x] **Step 1: Refresh drifted restart references**

Replace active `pm2 restart` usage with `pm2 task restart`, document no root
alias, and update the package map to `cmd/task/restart.go`.

- [x] **Step 2: Run fresh verification**

Run:

```bash
gofmt -w main.go main_test.go cmd/task/*.go
go test ./... -count=1
go test -race ./... -count=1
go build ./...
go vet ./...
go run . --help
go run . task --help
go run . task restart --help
git diff --check
```

Expected: all commands exit successfully; root help omits `restart`, task help
lists it, and all active usage uses `pm2 task restart`.

### Task 9: Move daemon commands into their domain package

**Files:**

- Modify: `main_test.go`
- Create: `cmd/daemon/daemon.go`
- Create: `cmd/daemon/start.go`
- Create: `cmd/daemon/start_alias.go`
- Create: `cmd/daemon/kill.go`
- Create: `cmd/daemon/stop.go`
- Create: `cmd/daemon/status.go`
- Create: `cmd/client_autostart.go`
- Modify: `cmd/client.go`
- Modify: `cmd/root.go`
- Delete: `cmd/daemon.go`
- Delete: `cmd/daemon_start.go`
- Delete: `cmd/daemon_kill.go`
- Delete: `cmd/daemon_stop.go`
- Delete: `cmd/daemon_status.go`
- Delete: `cmd/start.go`
- Modify: `main.go`

**Interfaces:**

- Consumes: the shared PM2 home/socket paths and daemon RPC protocol.
- Produces: package `cmd/daemon`, which owns the daemon command tree and its
  explicit root `pm2 start` alias.

- [x] **Step 1: Write a failing package-ownership test**

Assert that package `cmd/daemon` exists with one file per daemon command and
that the legacy daemon command files no longer exist in package `cmd`.

- [x] **Step 2: Verify RED**

Run:

```bash
go test . -run 'TestDaemonCommandsLiveInSubpackage$' -count=1
```

Expected: FAIL because `cmd/daemon` does not exist yet.

- [x] **Step 3: Move the command domain**

Move the daemon parent and start/kill/stop/status handlers into
`cmd/daemon`. Move the explicit root `pm2 start` alias with them. Keep silent
daemon auto-spawn in shared CLI-client infrastructure so package `cmd` does
not import its child and create an import cycle.

- [x] **Step 4: Verify GREEN**

Run:

```bash
go test . -run 'TestRootCmdCommandNamespaces$|TestDaemonCommandsLiveInSubpackage$' -count=1
```

Expected: PASS, with root `pm2 start` and nested `pm2 daemon start` sharing
the same handler.

### Task 10: Refresh and verify daemon package ownership

**Files:**

- Modify: `CLAUDE.md`
- Modify: `plans/2026-07-28-command-namespaces.md`

**Interfaces:**

- Consumes: the verified daemon package boundary.
- Produces: current technical documentation and a clean verified Go tree.

- [x] **Step 1: Refresh the package map**

Document `cmd/daemon` as the owner of daemon lifecycle commands and keep
shared auto-spawn behavior with the CLI client.

- [x] **Step 2: Run fresh verification**

Run:

```bash
gofmt -w main.go main_test.go cmd/root.go cmd/client.go cmd/client_autostart.go cmd/daemon/*.go
go test ./... -count=1
go test -race ./... -count=1
go build ./...
go vet ./...
go run . --help
go run . daemon --help
go run . daemon start --help
git diff --check
```

Expected: all commands exit successfully; root help retains only the explicit
`start` alias, daemon help retains all daemon lifecycle verbs, and no daemon
command implementation remains in package `cmd`.

### Task 11: Move wizard commands into their domain package

**Files:**

- Modify: `main_test.go`
- Create: `cmd/wizard/wizard.go`
- Create: `cmd/wizard/install.go`
- Create: `cmd/wizard/install_system.go`
- Create: `cmd/wizard/install_business.go`
- Create: `cmd/wizard/wizard_test.go`
- Delete: `cmd/eco.go`
- Delete: `cmd/eco_install.go`
- Delete: `cmd/eco_install_system.go`
- Delete: `cmd/eco_install_business.go`
- Delete: `cmd/eco_test.go`
- Modify: `main.go`

**Interfaces:**

- Consumes: `config/wizard` for prompt, render, merge, and install behavior.
- Produces: package `cmd/wizard`, which owns the `pm2 wizard` command tree and
  all Cobra-facing wizard integration tests.

- [x] **Step 1: Write a failing package-ownership test**

Assert that package `cmd/wizard` exists with one file per wizard command or
planner profile, and that the legacy `cmd/eco*.go` files no longer exist.

- [x] **Step 2: Verify RED**

Run:

```bash
go test . -run 'TestWizardCommandsLiveInSubpackage$' -count=1
```

Expected: FAIL because `cmd/wizard` does not exist yet.

- [x] **Step 3: Move the wizard command domain**

Move the thin Cobra wrapper, install subcommand, planner profile flag binders,
and CLI integration tests into `cmd/wizard`. Alias the existing
`config/wizard` import as `corewizard`, keep the command names and flags
unchanged, and register `wizardcmd.Cmd` from `main.go`.

- [x] **Step 4: Verify GREEN**

Run:

```bash
go test . ./cmd/wizard -run 'TestRootCmd|TestWizardCommandsLiveInSubpackage|TestWizard|TestInstall|TestPlanner' -count=1
```

Expected: PASS with `pm2 wizard` and `pm2 wizard install` retaining their
existing behavior.

### Task 12: Refresh and verify wizard package ownership

**Files:**

- Modify: `CLAUDE.md`
- Modify: `plans/2026-07-28-command-namespaces.md`

**Interfaces:**

- Consumes: the verified `cmd/wizard` boundary.
- Produces: current technical documentation and a clean verified Go tree.

- [x] **Step 1: Refresh the package map**

Document `cmd/wizard` as the Cobra-facing wizard package and retain
`config/wizard` as the interaction/rendering core.

- [x] **Step 2: Run fresh verification**

Run:

```bash
gofmt -w main.go main_test.go cmd/wizard/*.go
go test ./... -count=1
go test -race ./... -count=1
go build ./...
go vet ./...
go run . --help
go run . wizard --help
go run . wizard install --help
git diff --check
```

Expected: all commands exit successfully; root help retains `wizard`, wizard
help retains `install`, and no wizard command implementation remains directly
in package `cmd`.

### Task 13: Add explicit one-letter command aliases

**Files:**

- Modify: `main_test.go`
- Modify: `cmd/wizard/wizard.go`
- Modify: `cmd/save.go`
- Modify: `cmd/resurrect.go`
- Modify: `cmd/task/task.go`
- Modify: `cmd/daemon/daemon.go`
- Modify: `cmd/monitor.go`
- Modify: `cmd/list.go`

**Interfaces:**

- Consumes: the seven canonical root command singletons.
- Produces: exact one-letter aliases `w`, `s`, `r`, `t`, `d`, `m`, and `l`,
  with each alias named in the command's root-help description.

- [x] **Step 1: Write a failing alias contract test**

For each requested mapping, assert that `RootCmd.Find` resolves the alias to
the canonical command singleton and that `Short` contains
`short alias: pm2 <letter>`.

- [x] **Step 2: Verify RED**

Run:

```bash
go test . -run 'TestRootCmdShortAliases$' -count=1
```

Expected: FAIL because several aliases are absent and every command
description still omits the exact short-alias phrase.

- [x] **Step 3: Implement the aliases**

Add the exact alias to each Cobra command's `Aliases` field without removing
existing aliases, and append `(short alias: pm2 <letter>)` to `Short`.

- [x] **Step 4: Verify GREEN**

Run:

```bash
go test . -run 'TestRootCmdShortAliases$' -count=1
```

Expected: PASS for all seven mappings.

### Task 14: Synchronize and verify short-alias usage

**Files:**

- Modify: `README.md`
- Modify: `docs/usage.md`
- Modify: `skills/pm2/SKILL.md`
- Modify: `CLAUDE.md`
- Modify: `plans/2026-07-28-command-namespaces.md`

**Interfaces:**

- Consumes: the verified Cobra aliases and descriptions.
- Produces: one exact alias table across all active usage surfaces.

- [x] **Step 1: Refresh drifted usage descriptions**

Document all seven mappings and retain the separate explicit aliases
`pm2 start` and `pm2 apply`.

- [x] **Step 2: Run fresh verification**

Run:

```bash
gofmt -w main_test.go cmd/wizard/wizard.go cmd/save.go cmd/resurrect.go \
  cmd/task/task.go cmd/daemon/daemon.go cmd/monitor.go cmd/list.go
go test ./... -count=1
go test -race ./... -count=1
go build ./...
go vet ./...
go run . --help
go run . w --help
go run . s --help
go run . r --help
go run . t --help
go run . d --help
go run . m --help
go run . l --help
git diff --check
```

Expected: all commands exit successfully; root help names every short alias,
each one-letter command resolves, and active usage docs contain the same exact
mapping.

### Task 15: Create the custom root command package

**Files:**

- Modify: `main_test.go`
- Create: `cmd/root/root.go`
- Create: `cmd/root/execute.go`
- Create: `cmd/root/root_test.go`
- Move: `cmd/root.go` to `cmd/state.go`
- Modify: `main.go`

**Interfaces:**

- Consumes: shared commands from package `cmd` and the daemon/task/wizard
  command subpackages.
- Produces: `root.Cmd *cobra.Command` and `root.Execute(args []string) error`;
  `main.go` only forwards `os.Args[1:]` and maps an execution error to exit 1.

- [x] **Step 1: Write a failing package-ownership test**

Assert that `cmd/root` exists with `root.go`, `execute.go`, and
`root_test.go`; shared PM2 paths live in `cmd/state.go`; and the misleading
legacy `cmd/root.go` state file is absent.

- [x] **Step 2: Verify RED**

Run:

```bash
go test . -run 'TestCustomRootCommandLivesInSubpackage$' -count=1
```

Expected: FAIL because `cmd/root` and `cmd/state.go` do not exist yet.

- [x] **Step 3: Implement the custom root**

Move Cobra construction, gosdk config initialization, command registration,
traverse-hook configuration, and the metrics hook into `cmd/root/root.go`.
Move version argument dispatch into `cmd/root/execute.go`, preserving
`version`, `-v`, `--v`, `--version`, and `-version`. Reduce `main.go` to the
executable boundary.

- [x] **Step 4: Move and extend root tests**

Move command-tree/config/alias tests from package `main` to
`cmd/root/root_test.go`, keep repository structure tests in `main_test.go`,
and cover every preserved version spelling through `Execute`.

- [x] **Step 5: Verify GREEN**

Run:

```bash
go test . ./cmd/root -run 'TestCustomRootCommandLivesInSubpackage|TestRootCmd|TestExecute' -count=1
```

Expected: PASS with the existing command tree and version output owned by the
custom root package.

### Task 16: Refresh and verify the custom root boundary

**Files:**

- Modify: `CLAUDE.md`
- Modify: `plans/2026-07-28-command-namespaces.md`

**Interfaces:**

- Consumes: the verified `cmd/root` package.
- Produces: a current package map and fully verified executable bootstrap.

- [x] **Step 1: Refresh the technical package map**

Document `cmd/root` as the only Cobra composition root, `cmd/state.go` as
shared CLI runtime state, and `main.go` as the thin process exit boundary.

- [x] **Step 2: Run fresh verification**

Run:

```bash
gofmt -w main.go main_test.go cmd/state.go cmd/root/*.go
go test ./... -count=1
go test -race ./... -count=1
go build ./...
go vet ./...
go run . --help
go run . --version
go run . t --help
go run . d --help
go run . w --help
git diff --check
```

Expected: all commands exit successfully; help and aliases remain unchanged,
version prints `model.PM2Version`, and `main.go` contains no Cobra composition.
