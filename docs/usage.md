# pm2 — Usage Guide

A Go implementation of PM2-style process management.

---

## Installation

```bash
cd ~/projects/pm2
go build -buildvcs=false -o /usr/local/bin/pm2 .
```

The daemon starts automatically on first `pm2 task start`. Start it explicitly with
the short command or the daemon namespace:

```bash
pm2 start                       # same as: pm2 daemon start
pm2 start --foreground          # same as: pm2 daemon start --foreground
pm2 daemon start                # background (detached)
pm2 daemon start --foreground   # foreground (blocking; Ctrl+C stops it)
pm2 daemon kill                 # gracefully stop all processes and exit the daemon
```

> **Lifecycle verbs are split:** `pm2 task stop <name|id|all>` operates on a managed process (daemon keeps running). `pm2 daemon kill` operates on the daemon itself (everything exits). They share the same `executor.Stop` signal path (SIGTERM → 5s → SIGKILL); the only difference is the post-response `os.Exit(0)` hook in `daemon/network/handler.go:36-42`. The legacy top-level `pm2 kill` command has been removed; use `pm2 daemon kill`.

State directory: `~/.config/pm2/`

```tree
~/.config/pm2/
├── pm2.sock          # Unix socket for CLI ↔ daemon RPC
├── dump.json         # saved process list (pm2 save)
├── daemon.stopped    # marker written by pm2 daemon stop
├── logs/             # the daemon's own log
│   └── daemon.log
└── tasks/logs/       # every managed task's logs, one flat directory
    ├── <task-name>.log
    ├── <task-name>.err
    └── <task-name>.<YYYY-MM-DD>.log
```

---

## Command namespaces

Root command short aliases:

| Canonical command | Short alias |
| ----------------- | ----------- |
| `pm2 wizard` | `pm2 w` |
| `pm2 save` | `pm2 s` |
| `pm2 resurrect` | `pm2 r` |
| `pm2 task` | `pm2 t` |
| `pm2 daemon` | `pm2 d` |
| `pm2 monitor` | `pm2 m` |
| `pm2 list` | `pm2 l` |

Namespace aliases retain their subcommands, such as `pm2 t start` and
`pm2 d status`.

Explicit action aliases:

| Namespaced command | Explicit root alias |
| ------------------ | ------------------- |
| `pm2 daemon start` | `pm2 start` |
| `pm2 task start <config>` | `pm2 apply <config>` |
| `pm2 task restart <target>` | none |
| `pm2 task stop <target>` | none |
| `pm2 task pause <target>` | none |
| `pm2 task resume <target>` | none |
| `pm2 task delete <target>` | none |

---

## ecosystem.config — multi-process config file

### Recommended: `ecosystem.config.json`

JSON is fully supported and the most reliable format. Relative `script`
paths are resolved relative to the config file location (not CWD).

```json
{
    "apps": [
        {
            "name": "api-server",
            "script": "./bin/server",
            "args": ["--port", "8080"],
            "instances": 2,
            "env": {
                "NODE_ENV": "production",
                "PORT": "8080"
            },
            "cron_restart": "0 * * * *",
            "max_restarts": 10
        },
        {
            "name": "worker",
            "script": "./bin/worker",
            "instances": 4,
            "env": {
                "QUEUE": "default"
            }
        }
    ]
}
```

```bash
pm2 task start /path/to/ecosystem.config.json
```

### Also supported: `ecosystem.config.js`

The `.js` format is parsed via an embedded JS runtime (goja). It supports
pure ES2015+ syntax. Limitations:

- No `require()` / Node.js modules
- No `process.env` access
- No filesystem operations

```js
// ecosystem.config.js
module.exports = {
    apps: [
        {
            name: "api-server",
            script: "./bin/server", // relative to this config file
            args: ["--port", "8080"],
            instances: 2,
            env: {
                NODE_ENV: "production",
                PORT: "8080"
            },
            cron_restart: "0 * * * *",
            max_restarts: 10
        }
    ]
};
```

```bash
pm2 task start /path/to/ecosystem.config.js
```

### All `AppConfig` fields

| Field          | Type              | Default                      | Description                                                 |
| -------------- | ----------------- | ---------------------------- | ----------------------------------------------------------- |
| `name`         | string            | derived from script filename | Process identifier (must be unique)                         |
| `script`       | string            | required                     | Executable path (absolute or relative to config file)       |
| `args`         | []string          | `[]`                         | Arguments forwarded to the process                          |
| `instances`    | int               | `1`                          | How many copies to launch (named `<name>-0`, `<name>-1`, …) |
| `env`          | map[string]string | `{}`                         | Environment variables merged with inherited env             |
| `cron_restart` | string            | `""`                         | 5-field cron expression for scheduled restart               |
| `max_restarts` | int               | `15`                         | Auto-restart limit before giving up                         |

Log paths are not configurable: every task writes to
`~/.config/pm2/tasks/logs/<task-name>.log` and `<task-name>.err`.

---

### Workflows in the ecosystem file

```javascript
module.exports = {
    apps: [
        { name: "unit-tests", script: "./scripts/test.sh", optional: true }
    ],
    workflows: [
        {
            name: "nightly",       // required — no filename to derive one from
            category: "ci",        // grouping label; default "default"
            cron: "0 2 * * *",     // optional schedule for the whole workflow
            timeout: "30m",        // default ceiling for each stage
            cwd: "./repo",         // default working directory
            env: { CI: "1" },      // merged into every script stage
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

Each stage sets **exactly one** of `script`, `task`, or `workflow`; declaring
none or several is a load error that names what it actually found. `args`,
`env`, and `cwd` apply to a `script` stage only — on a `task` or `workflow`
stage they are rejected rather than ignored, because they would silently have
no effect.

A `task:` stage runs the registered task's command once and ignores
`instances`, `cron`, `cron_restart`, `watch`, `max_restarts`, `paused`, and
`optional`: those describe how a task is *supervised*, which has no meaning for
a single execution. If the referenced task is currently running as a service,
the stage starts a second process — declare it `optional: true` if it should
only ever run inside a workflow.

A `workflow:` stage runs the child inline and waits; the child's outcome is the
stage's outcome. Loops are refused three ways, and the third is the one that
matters:

1. A cycle among declared `workflow:` stages fails `pm2 apply`, naming the path.
2. A nested call to a workflow already on the chain fails that stage. Nesting
   is capped at 8.
3. **A workflow can only have one run in flight.** A stage script that calls
   `pm2 workflow run` or the webhook arrives as a brand-new request the first
   two guards cannot see; this one stops it.

A cron fire that lands on a running workflow is recorded `skipped` and dropped,
so the workflow runs late rather than being truncated. An explicit trigger gets
an error with the in-flight run's id instead — somebody is waiting for it.

## Full CLI reference

### `pm2 task start` / `pm2 apply`

```bash
pm2 task start [ecosystem.config.json|ecosystem.config.js|owner/repo] [flags]
pm2 apply [ecosystem.config.json|ecosystem.config.js|owner/repo] [flags]

Flags:
      --all            run optional apps instead of registering them paused
      --single         choose and apply exactly one app
      --with strings   run named optional apps instead of registering them paused
```

Examples:

```bash
# Ecosystem file (both formats)
pm2 task start ~/myapp/ecosystem.config.json
pm2 task start ~/myapp/ecosystem.config.js

# Default target: ./ecosystem.config.js
pm2 task start

# Explicit short alias; defaults to ./ecosystem.config.js
pm2 apply

# Interactively choose one app; other apps are not registered or changed
pm2 apply --single

# Remote GitHub repository
pm2 task start owner/repo
```

Task scripts, arguments, instances, environment variables, and cron schedules
are defined inside the ecosystem file.

`--single` can also be used with `pm2 task start`. It activates the chosen app
even if the app is marked `optional: true`, and cannot be combined with
`--all` or `--with`.

### `pm2 task stop`

```bash
pm2 task stop <name>      # stop by name
pm2 task stop all         # stop every process
```

Sends `SIGTERM`; if the process does not exit within 5 seconds, sends `SIGKILL`.
A deliberately stopped process is NOT auto-restarted even if `max_restarts > 0`.

### `pm2 task restart`

```bash
pm2 task restart <name>
pm2 task restart all
```

Performs a clean stop then immediately re-launches with the same config.
The `cron_restart` schedule is re-registered for the new process.

### `pm2 task delete`

```bash
pm2 task delete <name>
pm2 task delete all
```

Stops the process and removes it from the in-memory list. It will not appear
in `pm2 monitor` until started again. Does not affect `dump.json`.

### `pm2 logs`

```bash
pm2 logs              # stream every managed application
pm2 logs api-server   # stream one application
```

This continuously follows new current-file lines until `Ctrl+C`. stdout sources
stay on command stdout and stderr sources stay on command stderr. Output uses:

```text
[YYYY-MM-DD HH:MM:SS] app_name | escaped_log
```

Following starts at the current EOF and survives path replacement or
truncation. The managed-output escaping contract is documented under
[`pm2 logs`](../README.md#pm2-logs).

Every managed stdout/stderr line starts with `[YYYY-MM-DD HH:MM:SS]`.
`daemon.log` / `daemon.err` keep the latest date, while older leading date
blocks rotate beside them as `daemon.<YYYY-MM-DD>.log` / `.err`.

### `pm2 logs monitor` / `pm2 logs m`

```bash
pm2 logs monitor              # open the application/log-file Tree Explorer
pm2 logs m api-server         # initially select one application
```

The application/log-file Tree stays on the left and the selected log stays on
the right. Application rows begin with `[<id>]`; current files use `🔶`.
Use `→` to expand an application or load a file, or press `Enter` on a file to
load it and focus the right-hand Viewer. `↑`/`↓` or `j`/`k` controls whichever
pane has focus. `←` returns Viewer focus to the Tree or collapses a Tree branch;
`PageUp`/`PageDown` moves one Viewer page. On a Tree file row, `d` opens
deletion confirmation; only `y` deletes, while `n`/`Esc` cancels. The Viewer
does not expose delete.

### Go channel integration

External Go services can follow the same files without invoking the CLI:

```go
sources := []logfile.Source{{
	AppName: "api-server",
	Path:    "/path/to/daemon.log",
	Stream:  logfile.StreamStdout,
}}
entries, errs := logfile.Follow(ctx, sources)

for entries != nil || errs != nil {
	select {
	case entry, ok := <-entries:
		if !ok {
			entries = nil
			continue
		}
		fmt.Println(entry.String())
	case err, ok := <-errs:
		if !ok {
			errs = nil
			continue
		}
		handle(err)
	}
}
```

`Entry` exposes `Time`, `AppName`, `Stream`, and `Message`. Its `String`
method returns `[YYYY-MM-DD HH:MM:SS] app_name | escaped_log`.

### `pm2 taskmanager` (alias: `pm2 tm`)

```bash
pm2 taskmanager # short alias: pm2 tm
```

The system activity monitor. `pm2 monitor` scopes itself to managed
applications and their logs; `pm2 taskmanager` scopes itself to the machine.

- Top panel: CPU (user/system split, load average), memory (used,
  available, swap), network throughput, disk I/O and filesystem capacity.
- Left pane: pm2 tasks, or every OS process with `a`.
- Right pane: the selection's sub-processes and the ports its whole tree
  listens on.

Keys: `↑↓` / `jk` navigate, `PgUp` / `PgDn` page, `g` / `G` top / bottom,
`a` toggle scope, `s` cycle sort (cpu → memory → name), `q` quit.

The task list needs a running daemon; nothing else does. Without one the
header shows `daemon unreachable — showing system only` and the machine
panel keeps working.

Task CPU and memory are reported twice: the task's own process, and the
`tree` total including every descendant. A managed shell script that execs
the real worker shows near-zero usage on its own row and the true cost in
the tree total. Ports are collected across the whole tree for the same
reason — the socket usually belongs to a child, not to the process pm2
spawned.

### `pm2 taskmanager emit`

```bash
pm2 taskmanager emit --interval 30s
pm2 taskmanager emit --interval 1m --format text --out ~/.config/pm2/logs/dashboard.log
pm2 taskmanager emit --count 1 | jq '.tasks[] | select(.ports | length > 0)'
```

Emits one complete detection per interval rather than drawing it. Each
record carries the machine's resources plus every managed task with its
sub-processes and ports, so a consumer that misses a record loses one
sample instead of falling out of sync.

| Flag         | Default | Meaning                                          |
| ------------ | ------- | ------------------------------------------------ |
| `--interval` | `30s`   | Period between snapshots                         |
| `--count`    | `0`     | Stop after N snapshots; 0 runs until interrupted |
| `--out`      | stdout  | Append to this file instead of stdout            |
| `--format`   | `json`  | `json` (newline-delimited) or `text` (key=value) |

The first snapshot is written immediately so an interactive run shows
output straight away. `--out` appends, so restarts accumulate into one
stream instead of truncating history.

`json` writes one self-contained object per line. `text` writes
`[YYYY-MM-DD HH:MM:SS] scope=<host|disk|task|error> key=value` lines using
the same timestamp prefix as managed application logs, so the two read
consistently when tailed together. Values that can contain spaces — task
names, mount points, error messages — are quoted.

The daemon is never auto-started. A task list that cannot be read appears
in the snapshot's `errors` field and the machine readings still ship.

### `pm2 workflow`

Runs several tasks in order, stopping at the first failure. Workflows are
declared in the same ecosystem file, under a `workflows:` key beside `apps:`.

```bash
pm2 workflow list                     # declared workflows + latest outcome
pm2 workflow run ci:nightly           # trigger; returns as soon as it starts
pm2 workflow run ci:nightly --wait    # block, print every stage, exit non-zero on failure
pm2 workflow runs --limit 20          # history across all workflows
pm2 workflow runs ci:nightly          # history for one
pm2 workflow show <run-id>            # one run and its stages
pm2 workflow show <run-id> --logs     # ... plus each stage's captured output
```

`runs` and `show` read the run journal on disk and never open the socket, so
they work with the daemon down and still find the history of a workflow you
deleted. Only `run` may auto-start a daemon.

`<ref>` is `category:name`, or a bare `name` when unambiguous. `pm2 workflow`
has no short alias.

A stage runs **exactly once**; success is exit code 0 and nothing else.
`max_restarts`, `cron_restart`, `watch` and `instances` do not apply to a
stage, and a stage never enters the process registry — so it does not appear
in `pm2 list`, and `pm2 logs <name>` cannot follow it. Use
`pm2 workflow show --logs`, or `tail -f` the path it prints.

### `pm2 web`

```bash
pm2 web              # print the dashboard URL and open a browser
pm2 web --no-open    # print only
```

The dashboard is served by the daemon itself; this command only finds it. It
shows the live task table (click a row for that task's trigger history with
exit codes), the declared workflows, and a merged timeline of workflow and task
runs.

⚠️ **The dashboard and its webhook bind `0.0.0.0:8301` and check no
credential.** Anyone who can reach that port can trigger a workflow, and a
stage runs a shell command. Restrict or disable it at daemon start:

```bash
pm2 daemon start --web-host 127.0.0.1   # loopback only
pm2 daemon start --web-port 0           # no web server at all
```

The same values can come from `APP_WEB_HOST` / `APP_WEB_PORT`. Use the flat
form — a nested config key such as `web.port` is silently ignored.

Triggering a run over HTTP:

```bash
curl -X POST http://<host>:8301/api/webhooks/ci:nightly \
     -H 'Content-Type: application/json' \
     -d '{"params": {"DATE": "2026-08-28"}}'
```

`Content-Type: application/json` is required. Responses: `202` accepted (with
`Location` pointing at the run), `400` malformed body, `404` unknown workflow,
`409` a run is already in flight, `415` wrong content type, `429` more than ten
triggers a minute. The `202` does not echo the params back.

Every other endpoint is read-only. There is no HTTP route that restarts, stops,
or deletes a task.

### `pm2 save` / `pm2 s`

```bash
pm2 save
pm2 s
```

Persists the current process list to `~/.config/pm2/dump.json`, including all fields
needed to restore processes exactly (`cron_restart`, `env`, `args`, etc.).

### `pm2 resurrect` / `pm2 r`

```bash
pm2 resurrect
pm2 r
```

Reads `~/.config/pm2/dump.json` and starts every entry. Use this after a reboot to
restore your last-saved process list. Typically called from the startup script.

### `pm2 startup`

```bash
pm2 startup
```

Generates an OS-specific init script:

- macOS → `~/Library/LaunchAgents/com.shuk.pm2.plist`
- Linux → `~/.config/systemd/user/pm2.service`

The generated script starts `pm2 daemon` on login. After generating, activate it:

```bash
# macOS
launchctl load ~/Library/LaunchAgents/com.shuk.pm2.plist

# Linux
systemctl --user enable pm2
systemctl --user start pm2
```

Then save your current processes so `resurrect` is called on daemon start:

```bash
pm2 save
```

---

## Process identity and override behavior

A process is identified by the combination of **name + script path**.
Both must match for an override to be allowed.

Re-running an ecosystem file with the same name and script stop-and-replaces
the existing entry. If the same name points at a different script, the daemon
returns an error.

To replace a process with a different binary, delete it first:

```bash
pm2 task delete api
pm2 task start ./ecosystem.config.js
```

When running an ecosystem file with `instances > 1`, instances are named
`<name>-0`, `<name>-1`, … and each is independently identified:

```bash
pm2 task stop worker-0    # stops only the first worker instance
pm2 task restart worker-1 # restarts only the second
pm2 task stop all         # stops every process
```

Re-running `pm2 task start ecosystem.config.json` when the apps are already running
will stop-and-replace each entry by name.

---

## Relative paths in ecosystem.config

Relative `script` paths are resolved relative to the config file's directory,
not the shell's current working directory:

```tree
/home/user/myapp/
├── ecosystem.config.json   ← script: "./bin/server"
└── bin/
    └── server              ← resolved to /home/user/myapp/bin/server
```

```bash
# Works from any directory:
pm2 task start /home/user/myapp/ecosystem.config.json
cd /tmp && pm2 task start /home/user/myapp/ecosystem.config.json  # same result
```

> Paths are resolved at parse time in the CLI process before being sent to
> the daemon as absolute paths. When `cwd` is omitted, the config file's
> directory is also used as the process working directory and is shown in
> `pm2 m`. The daemon always receives absolute paths.

---

## How failed/exited processes are monitored

Each launched process has a dedicated `watchProcess` goroutine that calls
`cmd.Wait()` — this blocks until the OS process exits. No polling is used.

```tree
  daemon
    └── launchProcess()
          ├── cmd.Start()                  ← OS process spawned
          └── go watchProcess(mp) ─────────────────────────┐
                                                           │ blocks on cmd.Wait()
                                                           │
                                         process exits ───┘
                                           │
                              exit code 0? ──→ Status: stopped  (no restart)
                              exit code ≠0? ──→ Status: errored
                                              └─ stopping==true? → no restart
                                              └─ restarts < max_restarts?
                                                   ├── YES → sleep 30s → re-launch
                                                   └── NO  → Status: errored (final)
```

Key rules:

- Zero exit code → `stopped` — treated as intentional, never auto-restarted
- Non-zero exit code → `errored` → auto-restarted with 30 seconds delay
- `pm2 task stop` sets a `stopping` flag before sending SIGTERM — even though the
  process exits non-zero (killed), the flag suppresses auto-restart
- Counter `restarts` accumulates across the life of the entry (not reset on
  `pm2 task restart`); `max_restarts` default is 15

---

## How `cron_restart` works

`cron_restart` schedules a forced restart at a given time, independent of
whether the process is healthy or crashed.

```tree
  ecosystem.config.json
    └── cron_restart: "0 * * * *"
                              │
  daemon.launchProcess() ─────┘
    └── cron.Scheduler.Register("api-server", "0 * * * *", fn)
                              │
                              │   robfig/cron ticks each minute,
                              │   compares wall clock to schedule
                              │
                     schedule fires
                              │
                    restartByName("api-server")
                              ├── stopProcess()   ← SIGTERM + stopping=true
                              │                      cron entry removed here
                              └── launchProcess() ← new process + new cron entry
```

Cron expression format (5 fields, standard Unix cron):

```tree
┌───── minute (0–59)
│ ┌─── hour (0–23)
│ │ ┌─ day of month (1–31)
│ │ │ ┌ month (1–12)
│ │ │ │ ┌ day of week (0–6, Sunday=0)
│ │ │ │ │
* * * * *
```

Common examples:

| Expression    | Meaning                     |
| ------------- | --------------------------- |
| `*/5 * * * *` | Every 5 minutes             |
| `0 * * * *`   | Every hour on the hour      |
| `0 0 * * *`   | Every day at midnight       |
| `0 2 * * 0`   | Every Sunday at 02:00       |
| `30 6 1 * *`  | 1st of every month at 06:30 |

> The cron entry is removed when the process is stopped or deleted, and
> re-registered when it is restarted. So `pm2 task stop` cancels the schedule
> until the process is started again.

---

## Typical workflow

```bash
# 1. Build and install
go build -buildvcs=false -o /usr/local/bin/pm2 .

# 2. Write your config
cat > ~/myapp/ecosystem.config.json <<'EOF'
{
  "apps": [
    {
      "name": "api",
      "script": "./bin/server",
      "instances": 2,
      "env": { "PORT": "8080", "ENV": "production" },
      "cron_restart": "0 3 * * *",
      "max_restarts": 5
    }
  ]
}
EOF

# 3. Run tasks
pm2 task start ~/myapp/ecosystem.config.json

# 4. Check status
pm2 list

# 5. Stream logs
pm2 logs api

# Or browse/manage log files interactively
pm2 logs m api

# 6. Save for resurrection on reboot
pm2 save

# 7. Generate and activate startup script (macOS)
pm2 startup
launchctl load ~/Library/LaunchAgents/com.shuk.pm2.plist

# 8. Rolling restart after a deploy
pm2 task restart api

# 9. Teardown
pm2 task delete all
```
