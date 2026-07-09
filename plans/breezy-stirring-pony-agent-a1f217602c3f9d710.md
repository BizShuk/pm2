# pm2 Codebase Quality Improvements — Implementation Plan

Repo: `/Users/bytedance/projects/tmp/pm2` (Go 1.24+, module `github.com/bizshuk/pm2`)

This plan is organised into four independent phases. Each phase is gated by a verification step (`go build ./...`, `go vet ./...`, `go test ./...`) so a failed phase can be detected and rolled back without contaminating later phases.

Conventions used below:
- "Edit" means use the in-place edit tool to replace a specific range of lines; "Write" means create a new file (only `launch.go`, `lifecycle.go`, `keys.go`, `commands.go` are net-new in this plan).
- All file paths are absolute.
- Line numbers refer to the file as it exists today; they may drift by a few lines as edits within a phase land. Use the surrounding context to anchor each edit.

---

## Phase 1 — Dead Code Removal

Goal: remove symbols that the codebase no longer uses, without changing any behaviour. Pure deletion; tests must continue to pass.

### Step 1.1 — Remove `process.DumpEntry`

- Edit `/Users/bytedance/projects/tmp/pm2/process/types.go`, delete lines 152-178 (the entire `DumpEntry` struct definition plus its 7-line comment block above it). Do not touch anything else in `types.go`.
- Edit `/Users/bytedance/projects/tmp/pm2/CLAUDE.md`, line 266: change `├── dump.json       serialised []process.DumpEntry (pm2 save / resurrect)` to `├── dump.json       serialised []process.AppConfig (pm2 save / resurrect)`.
- Edit `/Users/bytedance/projects/tmp/pm2/README.todo`:
  - Line 70 — remove the trailing "`DumpEntry` 與" reference so the sentence reads "利用 anonymous embedding 簡化 `AppStartReq` 與 `ProcessInfo` 的定義與映射程式碼".
  - Line 73 — remove the "`process.DumpEntry` 標記 Deprecated，" clause from the result description. Leave the surrounding "AppConfig 為唯一真相來源… `process.ProcessInfo` 改為 embed `AppConfig` + 9 個 runtime 欄位" sentence intact.
  - Line 113 — remove the "`於鎖外組裝 `DumpEntry` 與寫檔" parenthetical so the sentence reads "於鎖外組裝 AppConfig 快照並寫檔".

Do NOT touch any `plans/` or `docs/specs/` markdown — those are historical design documents and the references in them are accurate for the era they describe.

### Step 1.2 — Remove `model.CmdLogs` and `Request.Follow`

- Edit `/Users/bytedance/projects/tmp/pm2/model/protocol.go`:
  - Line 33: delete the line `CmdLogs     CommandType = "logs"`.
  - Line 47: delete the field `Follow  bool        `json:"follow,omitempty"` from the `Request` struct.
- Edit `/Users/bytedance/projects/tmp/pm2/model/protocol_test.go`:
  - Lines 39-43: delete the entire "with follow" test case (the struct literal `{name: "with follow", req: Request{Command: CmdLogs, Follow: true}, want: `{"command":"logs","follow":true}`}` block).
  - Line 59: change the round-trip field check from `back.ID != c.req.ID || back.Follow != c.req.Follow` to `back.ID != c.req.ID`.

### Step 1.3 — Verify Phase 1

Run from `/Users/bytedance/projects/tmp/pm2`:
1. `go build ./...` — must compile
2. `go vet ./...` — no warnings
3. `go test ./...` — all tests pass
4. `grep -rn "DumpEntry\|CmdLogs" /Users/bytedance/projects/tmp/pm2/ --include="*.go"` — must return zero matches
5. `grep -n "Follow" /Users/bytedance/projects/tmp/pm2/model/*.go` — must return zero matches

If step 4 or 5 finds anything, locate and remove the references before continuing.

---

## Phase 2 — Consistency Fixes

Goal: link the CLI to the daemon's version constant, deduplicate the not-found error string, and centralise the cron-key construction.

### Step 2.1 — Move `PM2Version` to `model` and reference from CLI

The CLI's `--version` flag currently hard-codes `"1.0.0"` at `/Users/bytedance/projects/tmp/pm2/cmd/root.go:23`, which will silently disagree with the daemon's `PM2Version` (defined at `/Users/bytedance/projects/tmp/pm2/daemon/server.go:19`) the next time the version is bumped.

Since `model/` is the cross-package data contract (already imported by both `cmd/` and `daemon/`), `PM2Version` belongs there.

- Edit `/Users/bytedance/projects/tmp/pm2/model/protocol.go`: add the constant near the top of the const block (immediately after `CmdStatus`, before the closing `)` of the const block at line 38):
  ```go
  // PM2Version is the version string both the CLI (`pm2 --version`)
  // and the daemon (CmdStatus payload) report. Kept in the model
  // package so there is one source of truth shared across processes.
  const PM2Version = "1.0.0"
  ```
- Edit `/Users/bytedance/projects/tmp/pm2/daemon/server.go`: delete the `const PM2Version = "1.0.0"` declaration at lines 15-19 and its preceding comment block (lines 15-18). The reference at `process_manager.go:310` (`Version: PM2Version,`) must now resolve to `model.PM2Version`, so add `"github.com/bizshuk/pm2/model"` to the import list of `process_manager.go` if it is not already there. (Confirmed: `model` is already imported at `process_manager.go:16`, so no new import is needed — only a usage update at line 310 is required, which Go does implicitly because `model` is already in scope and `PM2Version` is now in the `model` package.)
- Edit `/Users/bytedance/projects/tmp/pm2/daemon/process_manager.go`, line 310: change `Version: PM2Version,` to `Version: model.PM2Version,`.
- Edit `/Users/bytedance/projects/tmp/pm2/cmd/root.go`, line 23: change `fmt.Println("1.0.0")` to `fmt.Println(model.PM2Version)`. Confirm `"github.com/bizshuk/pm2/model"` is in the import list of `root.go` — it is not yet. Add it to the `import (...)` block at lines 3-10 (alphabetically between `fmt` and `os`):
  ```go
  "github.com/bizshuk/pm2/model"
  ```

### Step 2.2 — Deduplicate the "process or namespace not found" error

The exact string `"process or namespace not found: %s"` is constructed at 5 call sites in `process_manager.go` (lines 129, 143, 176, 199, 229). Replace with a package-level helper.

- Edit `/Users/bytedance/projects/tmp/pm2/daemon/process_manager.go`: add an `errors` import (it is not yet imported; add `"errors"` to the import block at lines 3-19, alphabetically between `encoding/json` and `fmt`).
- Edit `/Users/bytedance/projects/tmp/pm2/daemon/process_manager.go`: immediately after the import block (i.e. just before the `ManagedProcess` declaration at line 21), add:
  ```go
  // errProcessNotFound is returned by RPC handlers when the target
  // resolves to zero managed processes (wrong name, unknown
  // namespace, unknown ID, or "all" with an empty registry). Wrapped
  // with the target name by processNotFoundError so the user sees
  // what they typed.
  var errProcessNotFound = errors.New("process or namespace not found")

  func processNotFoundError(name string) error {
      return fmt.Errorf("%w: %s", errProcessNotFound, name)
  }
  ```
- Edit `/Users/bytedance/projects/tmp/pm2/daemon/process_manager.go` and replace the 5 inline `fmt.Errorf("process or namespace not found: %s", name)` lines (129, 143, 176, 199, 229) with `processNotFoundError(name)`. Use a single `Edit` per call site — the surrounding code is identical, so each edit will need a slightly different context window to disambiguate; alternatively use `replace_all` with the string `return fmt.Errorf("process or namespace not found: %s", name)` and confirm only the 5 expected sites match.

### Step 2.3 — Add a `cronKey` helper

The expression `ns + ":" + name` is constructed inline at 7+ sites across `daemon/process_manager.go` (lines 146, 180, 202, 233, 427, 428, 440, 470, 519, 547) and once in `daemon/process_registry.go:300`. Centralise it.

- Edit `/Users/bytedance/projects/tmp/pm2/daemon/helpers.go` (or, if `helpers.go` does not exist, add a new file `/Users/bytedance/projects/tmp/pm2/daemon/cron_key.go`): add
  ```go
  package daemon

  // cronKey is the canonical "namespace:name" key used by the
  // process registry, the cron scheduler, and the metrics collector.
  // Centralised so that if the key format ever needs to change
  // (e.g. to handle a name containing ':') there is exactly one
  // site to update.
  func cronKey(ns, name string) string {
      return ns + ":" + name
  }
  ```
  If `helpers.go` already contains unrelated helpers, prefer the new `cron_key.go` to keep the diff small and avoid touching unrelated lines.
- Edit `/Users/bytedance/projects/tmp/pm2/daemon/process_manager.go`:
  - Line 146: change `key := mp.Info.Namespace + ":" + mp.Info.Name` to `key := cronKey(mp.Info.Namespace, mp.Info.Name)`.
  - Line 180: same replacement.
  - Line 202: same replacement.
  - Line 233: change `pm.reg.Remove(mp.Info.Namespace + ":" + mp.Info.Name)` to `pm.reg.Remove(cronKey(mp.Info.Namespace, mp.Info.Name))`.
  - Line 427: change `pm.reg.processes[ns+":"+name] = mp` to `pm.reg.processes[cronKey(ns, name)] = mp`.
  - Line 428: change `if oldKey != "" && oldKey != ns+":"+name {` to `if oldKey != "" && oldKey != cronKey(ns, name) {`.
  - Line 440: change `cronKey := ns + ":" + name` to `cronKey := cronKeyVar(ns, name)` — no, simpler: rename the local to `ck` and call the helper: `ck := cronKey(ns, name)`, then update lines 444, 449, 451, 456, 459 to reference `ck` instead of `cronKey`.
  - Line 470: change `key := mp.Info.Namespace + ":" + mp.Info.Name` to `key := cronKey(mp.Info.Namespace, mp.Info.Name)`.
  - Line 519: same replacement.
  - Line 547: change `key := ns + ":" + name` to `key := cronKey(ns, name)`.
- Edit `/Users/bytedance/projects/tmp/pm2/daemon/process_registry.go`, line 299-300: change
  ```go
  if m, ok := r.processes[ns+":"+name]; ok {
      return m, ns + ":" + name, true
  }
  ```
  to
  ```go
  k := cronKey(ns, name)
  if m, ok := r.processes[k]; ok {
      return m, k, true
  }
  ```
- Sanity check: `grep -n '"' '+' '"' '+' '":"' '+' '""' /Users/bytedance/projects/tmp/pm2/daemon/*.go` (or simply `grep -n '"\:"' /Users/bytedance/projects/tmp/pm2/daemon/*.go`) should now return zero hits.

### Step 2.4 — Verify Phase 2

1. `go build ./...` — must compile
2. `go vet ./...` — no warnings
3. `go test ./...` — all tests pass
4. `go test -race ./daemon/...` — must still pass (the lock-escaper behavior must be unaffected)
5. `go build -o /tmp/pm2v2 . && /tmp/pm2v2 version` — must print `1.0.0` (proves the CLI is now linked to the constant)

---

## Phase 3 — File Splits (Single Responsibility)

Goal: break the two largest files (each above 500 lines) into smaller files grouped by responsibility. No behavioural change; the package boundary stays the same in both splits, so no import changes are needed in the consumers.

### Step 3.1 — Split `daemon/process_manager.go` into 3 files

Source file: `/Users/bytedance/projects/tmp/pm2/daemon/process_manager.go` (581 lines).

The split boundaries (line numbers refer to the *post-Phase-2* file, which will be ~5-10 lines shorter than 581 because of the error-dedup and cronKey helper):

- **Keep in `process_manager.go`** (trimmed):
  - Lines 1-19: package + imports (unchanged, except for the `errors` import added in Phase 2)
  - Line 21 onward: `ManagedProcess` struct, `ProcessManager` struct, `NewProcessManager`, lock delegates
  - The 12 RPC methods: `StartApp`, `StopByName`, `RestartByName`, `PauseByName`, `ResumeByName`, `DeleteByName`, `ListAll`, `Save`, `Resurrect`, `KillAll`, `Ping`, `Status`
  - `findProcesses` helper
  - `StartMetricsCollector`
  - `refreshMetrics`

- **Move to `/Users/bytedance/projects/tmp/pm2/daemon/launch.go`** (new file):
  - The entire `launchProcess` function, currently lines 329-465 in the source (the body that constructs the `ManagedProcess`, registers it in the registry, wires up `executor.Watch`, and registers cron schedules). Header at lines 325-328 (`// launchProcess is the ProcessManager-side wrapper...`) goes with the function.

- **Move to `/Users/bytedance/projects/tmp/pm2/daemon/lifecycle.go`** (new file):
  - `onProcessExit` (lines 467-515)
  - `stopProcess` (lines 517-544)
  - `triggerCron` (lines 546-566)

Procedure:
1. Create the two new files with the standard `package daemon` header and the minimal set of imports they each need:
   - `launch.go` needs: `fmt`, `log/slog`, `os/user`, `time`, `github.com/bizshuk/pm2/cron` (no — `cron` is not referenced directly; the helper uses `pm.scheduler.Register` which is method-call syntax), `github.com/bizshuk/pm2/daemon/executor`, `github.com/bizshuk/pm2/model`, `github.com/bizshuk/pm2/process`. The `slog` import is only used by the cron-restart / cron-parse error paths inside `launchProcess`; the `os/user` import is used to populate `currentUser`; `fmt` is used to wrap executor errors. Use `goimports` (or compile-and-read-the-error) to determine the final list.
   - `lifecycle.go` needs: `fmt`, `time`, `github.com/bizshuk/pm2/model`, `github.com/bizshuk/pm2/process`. `triggerCron` calls `pm.launchProcess`, which is in the same package, so no new import is needed for that.
2. Copy the function bodies (including their leading `//` doc comments) from `process_manager.go` into the new files. The function signatures do not change.
3. Delete those functions (and the blank line above each) from `process_manager.go`.
4. Run `go build ./...` — the package compiles as a single unit, so the imports in `process_manager.go` will shrink. Remove any imports that are now only used by the moved code (the diff is small enough to let `goimports` do it, or run `go build` and read the error list).

### Step 3.2 — Split `tui/model.go` into 3 files

Source file: `/Users/bytedance/projects/tmp/pm2/tui/model.go` (515 lines).

- **Keep in `model.go`** (trimmed):
  - All const blocks (lines 18-32)
  - All message types: `tickMsg`, `refreshMsg`, `logsMsg`, `actionMsg` (lines 36-55)
  - `Model` struct, `New`, `Init`, `Update` (lines 59-146)
  - `applyRefresh` (lines 148-172)
  - `recomputeNamespaces` (lines 174-213)
  - `applyNamespaceFilter` (lines 215-247)
  - `cycleNamespace` (lines 249-265)
  - `sortProcs` (lines 354-413)
  - `cycleSort` (lines 415-430)
  - `View` (lines 496-515)

- **Move to `/Users/bytedance/projects/tmp/pm2/tui/keys.go`** (new file):
  - `handleKey` (lines 267-343)
  - `pauseOrResume` (lines 345-352)

- **Move to `/Users/bytedance/projects/tmp/pm2/tui/commands.go`** (new file):
  - `doRefresh` (lines 434-447)
  - `readLogs` (lines 449-469)
  - `doAction` (lines 471-489)

Procedure:
1. Create the two new files. Each gets `package tui` and the imports it needs:
   - `keys.go` needs: `fmt`, `github.com/charmbracelet/bubbletea` (for `tea.Cmd` and `tea.Model` returns), `github.com/bizshuk/pm2/model` (for `model.CmdRestart` / `model.CmdPause` / etc.), `github.com/bizshuk/pm2/process` (for `process.Status`).
   - `commands.go` needs: `bufio`, `encoding/json`, `fmt`, `os`, `sort`, `time` (the `time` import is for the `tea.Tick` time-handler referenced via `tickMsg`, but `tickMsg` is declared in `model.go`; actually `commands.go` does not need `time` — only `model.go`'s `Init` does). Final list: `bufio`, `encoding/json`, `fmt`, `os`, `sort`, `github.com/charmbracelet/bubbletea`, `github.com/bizshuk/pm2/model`, `github.com/bizshuk/pm2/process`.
2. Copy the function bodies (including their leading `//` doc comments) into the new files.
3. Delete the moved functions from `model.go` and prune any now-unused imports.

### Step 3.3 — Verify Phase 3

1. `go build ./...` — must compile (this is the load-bearing check for both file splits)
2. `go vet ./...` — no warnings
3. `go test ./...` — all tests pass
4. `go test -race ./daemon/...` — must pass. The split of `process_manager.go` is purely lexical, but `pause_race_test.go` (already in `daemon/`) exercises the lock + auto-restart paths that touch `onProcessExit` and `stopProcess`, so the move into `lifecycle.go` must be verified under the race detector.
5. `go test -race ./tui/...` — must pass. The `tui/model_test.go` exercises `View` / `Update` / `handleKey`, all of which are now spread across 3 files; the race detector will catch any accidental shared state.
6. `wc -l /Users/bytedance/projects/tmp/pm2/daemon/*.go /Users/bytedance/projects/tmp/pm2/tui/*.go` — confirm no single file in either directory is over ~450 lines.

If step 4 or 5 surfaces a regression, the most likely cause is a missing import in one of the new files (e.g. `slog` in `launch.go` if the cron-parse error path was moved without the import). Re-check the imports against the function bodies.

---

## Phase 4 — Folder Structure Cleanup

Goal: remove duplicated, orphaned, or scratch artifacts from the repo. This phase is independent of Phases 2-3 (Phase 1 already removed all the type-level dead code; this phase is the file-level dead code).

### Step 4.1 — Remove the orphaned `docs/superpowers/` directory

- The directory contains a single file `2026-06-13-process-namespace-design.md` (in `docs/superpowers/specs/`).
- Verified: the design content is already covered by `docs/specs/process-namespace-plan.md` and `docs/specs/architecture-evolution-pm2.md`. The `docs/superpowers/specs/2026-06-17-monitor-highlight-design.md` file has the same relationship to `docs/specs/2026-06-17-monitor-highlight.md`.
- Action: `rm -rf /Users/bytedance/projects/tmp/pm2/docs/superpowers/`. This is a 2-file delete; no other file in the repo references the `docs/superpowers/` path (verified by `grep -r "superpowers" /Users/bytedance/projects/tmp/pm2/` which would otherwise need updating).

### Step 4.2 — Clean root-level scratch artifacts

- `/Users/bytedance/projects/tmp/pm2/ecosystem.config.js` (922 bytes): a scratch config with hardcoded `/Users/shuk/...` paths and an active `cron_test` entry. Not a real ecosystem definition (the active process list would not run on any other machine). Two options:
  - **Preferred:** delete it (`rm /Users/bytedance/projects/tmp/pm2/ecosystem.config.js`). It is not referenced from any test, Makefile, CI, or docs (`grep -rn "ecosystem.config" /Users/bytedance/projects/tmp/pm2/ --include="*.go" --include="*.yml" --include="Makefile"` returns zero hits).
  - Alternative: move it under `tmp/` if you anticipate wanting it for manual smoke tests.
- `/Users/bytedance/projects/tmp/pm2/run.sh` (34 bytes): a one-liner `ln -s ~/.pm2 ./tmp/`. Pure local-development scratch; it is not referenced from any build or test target.
  - Delete it (`rm /Users/bytedance/projects/tmp/pm2/run.sh`).

If the operator prefers a "move not delete" posture, both files can be moved into a new `/Users/bytedance/projects/tmp/pm2/tmp/` scratch directory, but deletion is cleaner and is the action that matches the "remove orphaned" framing of Phase 4.

### Step 4.3 — (Deferred, optional) Consider `config/remote.go` as a sub-package

`/Users/bytedance/projects/tmp/pm2/config/remote.go` lives as a peer of `config/ecosystem.go` even though it serves a different concern (remote daemon connection, used by the `ecosystem wizard` sub-package). Promoting it to `config/remote/remote.go` would mirror the existing `config/wizard/` sub-package structure and tighten the module boundary, but it is a larger refactor (the `Wizard` type, the `Env` struct, the `cobra` command registration, and the test files would all need to move). Defer to a separate plan; this is documented here only for completeness so the next reader knows the inconsistency is intentional, not accidental.

### Step 4.4 — Verify Phase 4

1. `go build ./...` — must still compile (none of the removed files were Go source, so this should be a no-op)
2. `go test ./...` — all tests pass
3. `ls /Users/bytedance/projects/tmp/pm2/docs/superpowers 2>&1` — must report "No such file or directory"
4. `ls /Users/bytedance/projects/tmp/pm2/ecosystem.config.js /Users/bytedance/projects/tmp/pm2/run.sh 2>&1` — both must report "No such file or directory"
5. `git -C /Users/bytedance/projects/tmp/pm2 status` — should show only the expected removals and edits

---

## Final Verification (after all four phases)

From `/Users/bytedance/projects/tmp/pm2`:

1. `go build ./...` — must compile
2. `go vet ./...` — no warnings
3. `go test ./...` — all tests pass
4. `go test -race -count=2 ./...` — race detector must be clean
5. `golangci-lint run ./...` (if available) — no regressions versus the pre-change baseline
6. `go build -o /tmp/pm2bin .` — produces a working binary
7. Smoke test:
   - `rm -rf ~/.pm2 && /tmp/pm2bin daemon start`
   - `sleep 1 && /tmp/pm2bin start --name smoke --script /bin/sh -- -c "while true; do echo hi; sleep 1; done"`
   - `/tmp/pm2bin list` — must show the `smoke` process in `online` status
   - `/tmp/pm2bin stop smoke` — must succeed
   - `/tmp/pm2bin daemon kill` — daemon exits cleanly
8. `/tmp/pm2bin version` — must print `1.0.0` (proves the Phase 2.1 link between CLI and daemon is live)

If any smoke step fails, the most likely culprit is a stale import in one of the moved files. Run `goimports -w <file>` on the file under suspicion and rebuild.

---

## Sequencing & dependencies between phases

- Phases 1, 2, 3, 4 are independent and can be done in this exact order.
- Phase 1 must run first because it removes a struct (`DumpEntry`) referenced in two doc files. If a later phase edits the same doc files, the conflicts are easier to reason about if the deletion is already in.
- Phase 2 (line numbers) anchors against the post-Phase-1 file. Phase 3 (line numbers) anchors against the post-Phase-2 file. The line-number drift between phases is small (single-digit lines) and the surrounding context in each `Edit` is unique enough that it does not matter.
- Phase 4 is independent of all code phases and could be done at any time, but it is listed last so the operator sees the full picture before deleting non-Go artifacts.

---

## Critical Files for Implementation

- /Users/bytedance/projects/tmp/pm2/process/types.go
- /Users/bytedance/projects/tmp/pm2/model/protocol.go
- /Users/bytedance/projects/tmp/pm2/daemon/process_manager.go
- /Users/bytedance/projects/tmp/pm2/tui/model.go
- /Users/bytedance/projects/tmp/pm2/daemon/server.go
