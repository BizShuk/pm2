---
name: pm2
description: Use when managing background processes, daemon lifecycle, cron schedules, ecosystem files, logs, saved process state, or PM2 application configuration through the pm2 command-line tool.
---

# pm2 Commands

## Overview

A reference guide for the Go-based `pm2` command-line utility in this project.
Its command tree differs from the Node.js PM2 CLI, so use the commands and
fields documented here instead of assuming upstream PM2 compatibility.

## When to Use

- Spawning, stopping, killing, or checking the status of the `pm2` daemon.
- Registering, starting, stopping, restarting, pausing, resuming, or deleting processes.
- Tailing or viewing application logs.
- Saving the registered process list or resurrecting it after daemon startup.
- Inspecting or updating layered PM2 application settings with `pm2 config`.
- Designing ecosystem files interactively or installing planner agents.
- Chaining several tasks into an ordered workflow, running one, or reading a
  workflow's run history.
- Triggering a workflow from an external system through the HTTP webhook.
- Reading a task's trigger history: when it fired, what it exited with.

When NOT to use:

- Modifying the internal Go codebase of the daemon or executor.

## Command Reference

### Root Short Aliases

| Canonical command | Short alias |
| ----------------- | ----------- |
| `pm2 wizard`      | `pm2 w`     |
| `pm2 save`        | `pm2 s`     |
| `pm2 resurrect`   | `pm2 r`     |
| `pm2 task`        | `pm2 t`     |
| `pm2 daemon`      | `pm2 d`     |
| `pm2 monitor`     | `pm2 m`     |
| `pm2 list`        | `pm2 l`     |

`pm2 workflow` and `pm2 web` have no short alias — the alias table above is
the product's, not a pattern to extend.

Root help renders these as dedicated command, alias, and description columns.
Namespace aliases retain their subcommands, such as `pm2 t pause` and
`pm2 d status`.

`pm2 start` and `pm2 apply` are separate, explicit root aliases:

- `pm2 start` is equivalent to `pm2 daemon start`.
- `pm2 apply [target]` is equivalent to `pm2 task start [target]`.

Task lifecycle verbs have no other root aliases. Use `pm2 task restart`, not
`pm2 restart`; the same rule applies to `stop`, `pause`, `resume`, and
`delete`.

| Command                                          | Purpose                                          | Usage / Key Flags                                                                |
| ------------------------------------------------ | ------------------------------------------------ | -------------------------------------------------------------------------------- |
| `pm2 start` / `pm2 daemon start`                 | Spawn the daemon process                         | `--foreground` to run blocking in foreground                                     |
| `pm2 task start [target]` / `pm2 apply [target]` | Apply apps from an ecosystem file or GitHub repo | Defaults to `./ecosystem.config.js`; accepts `--all`, `--with`, `--single`       |
| `pm2 task restart <name\|id\|all>`               | Restart a task                                   | Closes, re-spawns, and re-registers scheduler                                    |
| `pm2 task stop <target>`                         | Stop a task gracefully                           | `SIGTERM` escalated to `SIGKILL` after 5 seconds                                 |
| `pm2 task pause <target>`                        | Suspend a task and its cron schedule             | Removes scheduler entries; status becomes `paused`                               |
| `pm2 task resume <target>`                       | Resume a paused task                             | Re-registers cron and launches the process                                       |
| `pm2 task delete <target>`                       | Remove a task from the registry                  | Removes configuration and stops process                                          |
| `pm2 list` / `pm2 l` / `pm2 ls` / `pm2 status`   | Print a non-interactive process table            | Bordered snapshot; `--no-color` for plain output                                 |
| `pm2 logs [name\|id\|namespace]`                 | Stream logs to stdout/stderr                     | Continues until Ctrl+C; output includes datetime and app name                    |
| `pm2 save`                                       | Persist current app configs                      | Saves to `~/.config/pm2/dump.json`                                                      |
| `pm2 resurrect`                                  | Restore saved app configs                        | Loads from `~/.config/pm2/dump.json`                                                    |
| `pm2 monitor` / `pm2 m`                          | Launch Bubbletea terminal dashboard              | `--sort name\|namespace\|cpu\|memory\|status`; no `-d` flag                      |
| `pm2 taskmanager` / `pm2 tm`                      | System activity monitor: host, tree, ports       | `a` toggles pm2 tasks / all processes; `s` cycles sort; works without a daemon   |
| `pm2 taskmanager emit`                         | Emit a full system snapshot on a fixed period    | `--interval`, `--count`, `--out`, `--format json\|text`; never auto-starts daemon |
| `pm2 logs monitor` / `pm2 logs m`                | Open the split Tree and log Viewer               | Enter focuses right Viewer; focused-pane navigation; Tree-file delete uses `y/N` |
| `pm2 startup`                                    | Generate OS boot startup scripts                 | Creates `plist` on macOS, systemd unit on Linux                                  |
| `pm2 config`                                     | Inspect or mutate layered application settings   | `--source`, `--update`, `--delete`, `--file`, `--local`                          |
| `pm2 daemon kill`                                | Gracefully exit all apps and daemon              | CLI commands can still auto-start the daemon                                     |
| `pm2 daemon stop`                                | Shutdown all apps, daemon and block auto-start   | Writes a stop marker to suppress auto-respawn                                    |
| `pm2 daemon status`                              | Read-only daemon status check                    | Works whether or not the daemon is running                                       |
| `pm2 wizard`                                     | Interactively build ecosystem config             | `--format`, `--output`, `--force`, `--no-merge`, `--yes`                         |
| `pm2 wizard install <script> [prompt]`           | Register a pre-configured planner agent          | Requires exactly one of `--system-planner`, `--business-planner`                 |

### `pm2 wizard` prompt flow

The interactive wizard asks for each app in this order:

1. Namespace — choose `Agent`, `Backup`, `Cloud`, `Local`, `Service`, or `AutoP`
2. Name
3. Script
4. Args
5. Instances
6. Watch mode
7. Environment variables
8. Cron schedule — blank skips; `r` chooses one daily time from 02:00 through
   08:00; any other value is kept as the custom cron expression
9. Cron restart — blank skips; `r` and custom cron expressions follow the same
   rules as the cron schedule
10. Max restarts — defaults to `15`
11. CWD — blank uses the ecosystem file directory
12. Optional — option 1 registers the app paused; option 2 makes it required
13. Add another app
14. Write to file

Blank namespace input selects `Agent`. Blank optional input selects option 1,
so the generated app has `optional: true` and is registered paused by
`pm2 task start`.

The wizard does not prompt for log paths, because there are none to set: every
task writes to `~/.config/pm2/tasks/logs/<task-name>.log` and `<task-name>.err`,
derived from the task name. `config_dir`, `log_file`, `out_file`, and
`error_file` no longer exist as ecosystem fields and are ignored if present.

The generated `name` field uses the uppercase convention
`NAMESPACE SCRIPT - NAME`. `SCRIPT` is the script filename without its path or
extension. For example, namespace `AutoP`, script `./worker.js`, and name
`daily sync` produce `AUTOP WORKER - DAILY SYNC`.

## Key Differences

### `pm2 task stop` vs `pm2 daemon kill` vs `pm2 daemon stop`

- `pm2 task stop` terminates a single managed process. The daemon keeps running.
- `pm2 daemon kill` terminates all managed processes, then gracefully exits the daemon itself. Subsequent CLI commands can still auto-respawn it.
- `pm2 daemon stop` terminates all managed processes, exits the daemon, and writes a stop marker that blocks silent auto-spawning from other CLI commands. Use `pm2 daemon start` to clear the marker and allow auto-spawning again.

### `pm2 task pause` vs `pm2 task stop`

- Both commands stop a running child and remove its active cron schedule.
- `pm2 task stop` leaves the task in `stopped` state. `pm2 task resume` is a
  no-op for it; use `pm2 task restart` or re-apply its ecosystem config.
- `pm2 task pause` records the reversible `paused` state. `pm2 task resume`
  re-registers the schedule and launches regular processes again.

### `pm2 config` vs ecosystem configuration

- `pm2 config` manages the application's layered gosdk settings. With no
  mutation flags it prints the merged settings; writes target
  `~/.config/pm2/settings.local.json` by default.
- `ecosystem.config.js` and `ecosystem.config.json` define managed tasks. Apply
  them with `pm2 task start` or `pm2 apply`.
- `pm2 config` does not register, start, or modify ecosystem tasks.

## Ecosystem Configurations

Ecosystem files (`ecosystem.config.js` or `ecosystem.config.json`) define one
or more applications. Relative `script` paths resolve against the config
file's directory. When `cwd` is omitted, that directory also becomes the
task's working directory.

### User-Facing AppConfig Fields

| Field          | Type     | Default                           | Description                                           |
| -------------- | -------- | --------------------------------- | ----------------------------------------------------- |
| `name`         | string   | script filename without extension | Process name shown in `pm2 list`                      |
| `namespace`    | string   | `"default"`                       | Group label for organising processes                  |
| `script`       | string   | —                                 | Path or `$PATH`-resolvable command (required)         |
| `args`         | string[] | `[]`                              | Arguments passed to the script                        |
| `instances`    | int      | `1`                               | Number of process copies to spawn                     |
| `env`          | object   | `{}`                              | Environment variables as key-value pairs              |
| `cron`         | string   | `""`                              | 5-field cron expression for a one-shot scheduled task |
| `cron_restart` | string   | `""`                              | 5-field cron expression that restarts a running task  |
| `watch`        | bool     | `false`                           | Restart on script file changes via fsnotify           |
| `max_restarts` | int      | `15`                              | Non-zero-exit restart ceiling                         |
| `cwd`          | string   | ecosystem file directory          | Effective working directory for the child             |
| `optional`     | bool     | `false`                           | Register paused unless selected by start flags        |

This implementation does not define an `autorestart` field. A clean exit stays
stopped; a non-zero exit restarts up to `max_restarts`. Copying
`autorestart` from Node.js PM2 examples has no effect here.

`version`, `config_file`, `base_env`, and `paused` are runtime-managed fields,
not ecosystem choices. The wizard neither prompts for nor emits them.

### `cron` vs `cron_restart`

- `cron`: fires the script once per schedule; the process exits naturally after each run. Use for one-shot tasks (data sync, cleanup, audit).
- `cron_restart`: restarts a long-running process on schedule (e.g., daily memory reset). The process stays online between restarts.

### Usage Patterns

#### Pattern 1 — Long-running daemon (always-on service)

```javascript
{
    name: "LLM Proxy",
    namespace: "Service",
    script: "proxy",
    instances: 1,
    env: { PORT: "8080" }
}
```

Start and forget. pm2 auto-restarts on crash up to `max_restarts`.

#### Pattern 2 — One-shot cron task (daily scan)

```javascript
{
    name: "Disk Analysis Daily",
    namespace: "Local",
    script: "dux",
    args: ["scan"],
    cron: "0 6 * * *"
}
```

Runs once at 06:00 daily, exits, waits for the next schedule. Use `pm2 task pause` to suspend, `pm2 task resume` to reactivate.

#### Pattern 3 — Shell script with weekly schedule

```javascript
{
    name: "Launch Audit",
    namespace: "Local",
    script: "./bin/mac/launch_audit-mac.sh",
    cron: "0 5 * * 5"
}
```

Relative paths resolve against the ecosystem config file's directory, not the CWD.

#### Pattern 4 — AI agent planner with `__dirname`

```javascript
{
    name: "agy-system-planner",
    script: "agy",
    args: ["--add-dir", __dirname, "-p", "run /system-planner for current workspace"],
    namespace: "planner",
    instances: 1,
    cron: "10 0-9 * * *",
    watch: false
}
```

`__dirname` is available in `.js` configs through the embedded goja runtime.
The task remains idle between `cron` fires.

#### Pattern 5 — CLI tool with arguments (Go binary via `$PATH`)

```javascript
{
    name: "Golang Clean Cache",
    namespace: "Local",
    script: "go",
    args: ["clean", "-cache"],
    cron: "0 10 * * 5"
}
```

Bare command names are resolved via `$PATH`. No path prefix needed for globally-installed tools.

#### Pattern 6 — Always-on service with arguments

```javascript
{
    name: "Ollama",
    script: "ollama",
    namespace: "Agent",
    args: ["serve"],
    instances: 1
}
```

Background services that accept subcommands. Grouped under the `Agent` namespace.

#### Pattern 7 — Node.js app with env + `cron_restart`

```javascript
{
    name: "api-server",
    namespace: "Service",
    script: "./server.js",
    instances: 3,
    cron_restart: "0 4 * * *",
    env: { PORT: "3000", NODE_ENV: "production" }
}
```

`cron_restart` differs from `cron`: the process stays online and gets restarted at 04:00 daily (e.g., to reclaim leaked memory).

#### Pattern 8 — File watcher (restart on source changes)

```javascript
{
    name: "dev-server",
    namespace: "Local",
    script: "./main.go",
    watch: true,
    env: { DEBUG: "true" }
}
```

`watch: true` enables fsnotify-based restart whenever the script file changes. Useful during development.

#### Pattern 9 — Custom working directory

```javascript
{
    name: "agy-gosdk-system",
    script: "agy",
    args: ["--add-dir", "/Users/shuk/projects/tmp/gosdk", "-p", "'run /system-planner'"],
    namespace: "planner",
    cwd: "/Users/shuk/projects/tmp/gosdk",
    instances: 1,
    cron: "40 0-9 * * *"
}
```

`cwd` sets the working directory for the spawned process. Script paths are still resolved against the config file's directory.

#### Pattern 10 — Multiple cron tasks in the same repo

```javascript
{
    apps: [
        {
            name: "Golang Clean Cache",
            namespace: "Local",
            script: "go",
            args: ["clean", "-cache"],
            cron: "0 10 * * 5"
        },
        {
            name: "Golang Clean ModCache",
            namespace: "Local",
            script: "go",
            args: ["clean", "-modcache"],
            cron: "0 10 * * 5"
        }
    ];
}
```

Multiple apps in one config file. Each gets its own name, schedule, and lifecycle.

## Workflows

A workflow runs several stages in order, stopping at the first failure. It is
declared in the same `ecosystem.config.js`, under a new top-level `workflows:`
key beside `apps:`.

A stage runs **exactly once**. Success means exit code 0 and nothing else. None
of pm2's supervision behaviour applies to a stage — no auto-restart, no
`cron_restart`, no `watch`, no `instances`. A stage is an execution, not a
registration, so it never appears in `pm2 list`; use `pm2 workflow list` for a
run in flight and `pm2 workflow show` for one that finished.

### Commands

| Command | What it does | Needs the daemon |
| --- | --- | --- |
| `pm2 workflow list` | Declared workflows and their latest outcome | yes |
| `pm2 workflow run <ref> [--wait]` | Trigger one run | yes |
| `pm2 workflow runs [ref] [--limit N]` | Run history, read from disk | no |
| `pm2 workflow show <run-id> [--logs]` | One run, its stages, its output | no |
| `pm2 web [--no-open]` | Print and open the dashboard URL | yes |

`runs` and `show` read the journal directly, so history survives the daemon
being down and outlives a workflow you deleted. Only `run` may auto-start a
daemon — it is the only one that changes anything.

`<ref>` is `category:name`, or a bare `name` when it is unambiguous.

### Workflow fields

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `name` | string | — | Required; no filename to derive one from |
| `category` | string | `"default"` | Grouping label, the workflow analogue of `namespace` |
| `stages` | object[] | — | At least one; run in declaration order |
| `cron` | string | `""` | Schedule; a fire while a run is in flight is recorded `skipped` |
| `env` | object | `{}` | Merged into every script stage, under the stage's own `env` |
| `cwd` | string | ecosystem file directory | Default working directory for stages |
| `timeout` | string | `""` | Go duration (`30m`); default ceiling for each stage |

### Stage fields

Exactly one of `script`, `task`, or `workflow` per stage. Declaring none or
several is a load error naming what it actually found.

| Field | Applies to | Description |
| --- | --- | --- |
| `name` | all | Defaults to `stage-N` |
| `script` + `args` + `env` + `cwd` | script stage | An inline command |
| `task` | task stage | Run a registered task's command once |
| `workflow` | workflow stage | Run another workflow inline and wait for it |
| `timeout` | all | Overrides the workflow's |

`args`, `env`, and `cwd` on a task or workflow stage are a **load error**, not
a field that quietly does nothing: a task stage runs the referenced task's own
command and environment.

A `task:` stage ignores `instances`, `cron`, `cron_restart`, `watch`,
`max_restarts`, `paused`, and `optional` — those describe how a task is
supervised, which has no meaning for a single execution. Pairing
`optional: true` with a `task:` stage is the intended way to declare a task
that only ever runs as part of a workflow.

### Example

```javascript
module.exports = {
    apps: [
        { name: "unit-tests", script: "./scripts/test.sh", optional: true }
    ],
    workflows: [
        {
            name: "nightly",
            category: "ci",
            cron: "0 2 * * *",
            timeout: "30m",
            env: { CI: "1" },
            stages: [
                { name: "pull", script: "./scripts/pull.sh", args: ["--ff-only"] },
                { name: "test", task: "unit-tests" },
                { name: "ship", workflow: "ci:deploy" }
            ]
        },
        {
            name: "deploy",
            category: "ci",
            stages: [{ name: "push", script: "./scripts/push.sh" }]
        }
    ]
};
```

### Workflows calling workflows

A `workflow:` stage runs the child inline and waits for it; the child's outcome
is the stage's outcome. Three guards stop a loop:

1. A cycle among declared `workflow:` stages fails the whole `pm2 apply`,
   naming the path (`ci:a -> ci:b -> ci:a`).
2. A nested call to a workflow already on the current chain fails that stage.
   Nesting is capped at 8 deep.
3. **A workflow can only have one run in flight.** This is the guard that
   actually holds, because a stage script calling `pm2 workflow run` or the
   webhook arrives as a brand-new request the first two cannot see.

A cron fire that lands on a running workflow is recorded `skipped` and dropped
— the workflow runs late rather than being truncated and restarted. An explicit
trigger gets an error instead, because somebody is waiting for the answer.

## Webhook

`pm2 daemon start` also starts an HTTP server. `pm2 web` prints and opens
its URL; `pm2 daemon status` shows the address.

```bash
curl -X POST http://<host>:8301/api/webhooks/ci:nightly \
     -H 'Content-Type: application/json' \
     -d '{"params": {"DATE": "2026-08-28"}}'
# → 202 {"run_id":"20260828T030012-a1b2c3","workflow":"ci:nightly","status":"queued"}
```

`This endpoint is reachable from the network and checks no credential.`
Anyone who can reach port 8301 can trigger a workflow, and a workflow stage
runs a shell command — treat reachability to this port as equivalent to shell
access on the machine. Restrict it with `pm2 daemon start --web-host 127.0.0.1`,
or disable the server entirely with `--web-port 0`.

`Content-Type: application/json` is required. This is content negotiation, but
it also means a cross-origin POST from a page in someone's browser has to clear
a CORS preflight that pm2 never answers.

| Response | Meaning |
| --- | --- |
| `202` | Run started. `Location` points at it; poll rather than waiting on the connection |
| `400` | Malformed body, unknown field, or an invalid param name |
| `404` | No such workflow (the error does not list the known ones) |
| `409` | A run of this workflow is already in flight, with its run id |
| `415` | Missing or wrong `Content-Type` |
| `429` | More than 10 triggers for this workflow in a minute |
| `503` | Too many workflow runs in flight |

The 202 does not echo `params` back.

### HTTP endpoints

Everything else is read-only. `There is no task-mutating endpoint` — nothing
over HTTP can restart, stop, or delete a task.

```
GET  /                                            dashboard
GET  /healthz
GET  /api/status                                  daemon info
GET  /api/tasks                                   live task table
GET  /api/tasks/runs?name=&limit=&status=         task trigger history
GET  /api/workflows                               declared workflows
GET  /api/workflows/runs?limit=                   runs, in flight first
GET  /api/workflows/runs/{runID}                  one run and its stages
GET  /api/workflows/runs/{runID}/logs/{stage}     stage output (text/plain)
POST /api/webhooks/{workflow}                     trigger a run
```

Port `8301` and host `0.0.0.0` are the defaults; override with `--web-host` /
`--web-port`, or the flat env vars `APP_WEB_HOST` / `APP_WEB_PORT`. Nested
config keys such as `web.port` are silently ignored — always use the flat form.

## Run history

Both journals are append-only JSONL under the state directory, one line per
**finished** run:

```
~/.config/pm2/tasks/runs/YYYY-MM-DD.jsonl        task runs
~/.config/pm2/workflows/runs/YYYY-MM-DD.jsonl    workflow runs
~/.config/pm2/workflows/logs/<wf>.<runID>.<stage>.log
```

A run in flight is not in the journal — the daemon reports what is running, the
journal reports what finished. Files older than 30 days are pruned.

`exit_code` is `null`, not `0`, when there is no exit code to report — a launch
that never produced a child, or a process killed by a signal (whose `signal`
field carries the name). A cron task's `last_cron_status` is `ok` only when it
actually exited 0; `running` means launched with the outcome not yet known.

```bash
jq -c 'select(.status=="failed")' ~/.config/pm2/tasks/runs/$(date +%F).jsonl
```

## Workflow Examples

### Run all required apps from an ecosystem file

```bash
pm2 task start              # defaults to ./ecosystem.config.js
pm2 apply                   # explicit root alias; same default
pm2 apply --single          # choose and apply exactly one app
```

`--single` cannot be combined with `--all` or `--with`. It sends only the
chosen app, starts it even when `optional: true`, and leaves every other
registered app untouched.

### Suspend a cron task temporarily

```bash
pm2 task pause "Disk Analysis Daily"
# ... later ...
pm2 task resume "Disk Analysis Daily"
```

### Check what is running

```bash
pm2 list              # one-shot table
pm2 monitor           # interactive TUI dashboard
pm2 monitor --sort cpu
```

### Check what the machine is doing

```bash
pm2 taskmanager                          # host resources + process tree + ports (alias: pm2 tm)
pm2 taskmanager emit --count 1 | jq .system  # one machine-readable snapshot
pm2 taskmanager emit --interval 1m --format text --out ~/.config/pm2/logs/dashboard.log
```

`pm2 taskmanager` is the system activity monitor: CPU / memory / network /
disk on top, pm2 tasks (or every OS process, with `a`) below, and the
selection's sub-processes and listening ports on the right. It runs
without a daemon; only the task list needs one.

The monitor detail pane shows the executor-resolved effective `cwd`. For a
legacy saved task with no recorded `cwd`, it displays the directory containing
that task's `config_file` as a compatibility inference.

### Inspect or update application settings

```bash
pm2 config
pm2 config --source
pm2 config --update server.port=8080
pm2 config --delete server.host
pm2 config --local --update server.host=127.0.0.1
```

Mutation flags write `settings.local.json` under `~/.config/pm2/` unless
`--file` chooses another supported layer or `--local` targets the current
working directory.

### Persist and restore across reboots

```bash
pm2 save              # write ~/.config/pm2/dump.json
pm2 startup           # generate launchd/systemd service
# After reboot:
pm2 resurrect         # reload saved processes
```

### Stream, browse, and manage logs

```bash
pm2 logs             # continuously stream every managed application
pm2 logs "LLM Proxy" # continuously stream one application
```

Streamed stdout stays on command stdout and streamed stderr stays on command
stderr. Each line renders as `[YYYY-MM-DD HH:MM:SS] app_name | log` until
`Ctrl+C`.

```bash
pm2 logs monitor             # interactive Tree Explorer
pm2 logs m "LLM Proxy"       # same mode through the logs child alias
```

The application/log-file Tree stays on the left and the selected log stays on
the right. Application rows begin with `[<id>]`, and `🔶` marks each current
file. Use Right to expand an application or load a file. Enter on a file loads
it and focuses the right Viewer; `↑`/`↓` or `j`/`k` controls the focused pane,
Left returns focus to the Tree, and PageUp/PageDown moves a Viewer page.
Pressing `d` on a Tree file row opens confirmation; only `y` deletes it
(`n` / `Esc` cancels). Delete is unavailable while the Viewer has focus.

Managed stdout/stderr lines begin with `[YYYY-MM-DD HH:MM:SS]`. Current
`daemon.log` / `daemon.err` keep the latest date; prior leading date blocks
rotate beside them as `daemon.<YYYY-MM-DD>.log` / `.err`.

### Completely shut down

```bash
pm2 daemon kill        # stop all + exit daemon (auto-respawn OK)
pm2 daemon stop        # stop all + exit daemon + block auto-respawn
```

### Optional apps

An app may set `optional: true` to become opt-in. `pm2 task start` registers it in
the `paused` state and prints the command to resume it; everything else in the
file starts. This is useful when a repo ships both mandatory tasks (a daily
report) and tasks a given machine may not want (a planner agent).

```javascript
module.exports = {
    apps: [
        { name: "daily-report", script: "./report.sh", cron: "0 9 * * *" },
        { name: "planner", script: "./planner.sh", optional: true }
    ]
};
```

- `pm2 task start ./ecosystem.config.js` — starts `daily-report` and registers `planner` paused
- `pm2 task start ./ecosystem.config.js --with planner` — also starts `planner`
- `pm2 task start ./ecosystem.config.js --all` — starts every app
- `pm2 apply --single` — choose one app from `./ecosystem.config.js`; leave every other app untouched

`--with` is repeatable or comma-separated and matches `name` or
`namespace:name`. A name that matches no app is an error, so a typo does not
silently leave the app unstarted. The same rules apply to remote installs
(`pm2 task start owner/repo`).

`--single` is interactive, starts an explicitly selected optional app as
active, and cannot be combined with `--all` or `--with`.

## Common Mistakes

- Using `pm2 start ecosystem.config.js`. Root `pm2 start` starts the daemon;
  use `pm2 task start` or `pm2 apply` for ecosystem files.
- Calling removed root task verbs such as `pm2 restart` or `pm2 pause`. Put
  lifecycle actions under `pm2 task`.
- Confusing `pm2 task stop` with `pm2 daemon kill` or `pm2 daemon stop`. Always double check if you want to stop a single process or the entire daemon.
- Expecting `pm2 task resume` to reactivate a task that was stopped rather than
  paused.
- Starting a script with a duplicate name but different code. Use `pm2 task delete` first to remove the old entry.
- Using `cron` when you mean `cron_restart` (or vice versa). `cron` = one-shot scheduled run; `cron_restart` = restart an already-running process on schedule.
- Copying Node.js PM2's unsupported `autorestart` field into an ecosystem file.
- Using `pm2 config` to change process definitions; it manages application
  settings, not ecosystem tasks.
- Forgetting that relative `script` paths resolve against the config file's directory, not the shell's CWD when running `pm2 task start`.
- Putting `args`, `env`, or `cwd` on a `task:` or `workflow:` stage. Those apply
  only to a `script` stage and are rejected at load rather than ignored.
- Expecting a workflow stage to appear in `pm2 list`. Stages are one-shot
  executions and never enter the registry; use `pm2 workflow list` / `show`.
- Expecting a failing stage to be retried. `max_restarts` does not apply to a
  stage; exit non-zero and the run stops there.
- Setting `web.port` in the config file. Nested keys are silently ignored — use
  the flat `web_port` / `APP_WEB_PORT`.
- Assuming the webhook is protected because it is "internal". It is bound to
  every interface and checks no credential by default.
