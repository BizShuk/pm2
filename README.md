# pm2

A PM2-inspired process manager written in Go. Manages long-running processes with automatic restart, cron-based scheduling, a live TUI dashboard, a system activity monitor, and OS startup integration.

## Install

```bash
git clone https://github.com/bizshuk/pm2
cd pm2
go build -o /usr/local/bin/pm2 .
```

State directory: `~/.pm2/` (created automatically on first run)

---

## Commands

### Root command short aliases

| Canonical command | Short alias |
| ----------------- | ----------- |
| `pm2 wizard` | `pm2 w` |
| `pm2 save` | `pm2 s` |
| `pm2 resurrect` | `pm2 r` |
| `pm2 task` | `pm2 t` |
| `pm2 daemon` | `pm2 d` |
| `pm2 monitor` | `pm2 m` |
| `pm2 list` | `pm2 l` |

Namespace aliases retain their subcommands: for example, `pm2 t restart api`
is the short form of `pm2 task restart api`, and `pm2 d status` is the short
form of `pm2 daemon status`.

### `pm2 config`

Inspect the merged application configuration or update one of the configuration files managed by `gosdk/cmd`.

```bash
pm2 config
pm2 config --source
pm2 config --update server.host=0.0.0.0
pm2 config --delete server.host
pm2 config --file config.yaml --update server.port=8080
```

Writes target `~/.config/pm2/` by default; use `--local` to target the current working directory. The default mutation file is `settings.local.json`. This command manages application-level SDK configuration, while process definitions remain in `ecosystem.config.js` or `ecosystem.config.json`.

---

### `pm2 start` / `pm2 daemon start`

Start the PM2 daemon. The top-level command is the short form:

```bash
pm2 start                    # same as: pm2 daemon start
pm2 start --foreground       # same as: pm2 daemon start --foreground
```

### `pm2 task start` / `pm2 apply`

Run tasks from an ecosystem config file or remote GitHub repository. When no
target is given, `ecosystem.config.js` in the current directory is used.
`pm2 apply` is the explicitly supported short alias.

```bash
pm2 task start [ecosystem.config.js|ecosystem.config.json|owner/repo] [flags]
pm2 apply [ecosystem.config.js|ecosystem.config.json|owner/repo] [flags]

Flags:
      --all            run optional apps instead of registering them paused
      --delete         delete every task declared by the ecosystem file
      --single         choose and apply exactly one app
      --with strings   run named optional apps instead of registering them paused
```

```bash
pm2 task start ./ecosystem.config.json
pm2 apply ./ecosystem.config.json
pm2 apply --single
pm2 task start ./ecosystem.config.js
pm2 task start owner/repo
```

`pm2 apply --single` reads the current `./ecosystem.config.js`, shows its apps,
and applies only the selected app. The selected app starts active even when it
has `optional: true`; all other apps are left untouched. `--single` cannot be
combined with `--all` or `--with`.

`--delete` is the teardown counterpart of the same command: it loads the same
ecosystem file (default `./ecosystem.config.js`) and deletes every task the file
declares, addressing each one by its exact `namespace:name` key so a same-named
task from another ecosystem file is never touched. Apps the daemon does not know
are reported as `skipped` and the sweep continues; the command fails only when
no declared task was registered at all. `--delete` cannot be combined with
`--all`, `--with`, or `--single`.

```bash
pm2 apply --delete                              # tear down ./ecosystem.config.js
pm2 task start ./ecosystem.config.json --delete # canonical form, explicit file
```

Process identity is `name + script path`. Re-starting with the same name and script replaces the existing process. Re-starting with the same name but a different script returns an error — use `pm2 task delete` first.

An ecosystem app with `optional: true` is always registered, but starts in
`paused` state by default. It has no child process or active cron schedule until
resumed. Use `--all` to start every optional app immediately, or `--with
<name>` to start selected optional apps:

```bash
pm2 task start ./ecosystem.config.js                 # optional apps register paused
pm2 task resume default:planner                      # activate one registered app
pm2 task start ./ecosystem.config.js --with planner # start one optional app now
pm2 task start ./ecosystem.config.js --all           # start every optional app now
```

---

### `pm2 task` / `pm2 t`

The task namespace is the canonical home for task lifecycle commands:

| Canonical command | Short alias | Purpose |
| ----------------- | ----------- | ------- |
| `pm2 task start <config>` | `pm2 t start <config>`; `pm2 apply <config>` | Register and start tasks |
| `pm2 task restart <target>` | `pm2 t restart <target>` | Restart a task with its stored config |
| `pm2 task stop <target>` | `pm2 t stop <target>` | Stop a task |
| `pm2 task pause <target>` | `pm2 t pause <target>` | Pause a task and its cron schedule |
| `pm2 task resume <target>` | `pm2 t resume <target>` | Resume a paused task |
| `pm2 task delete <target>` | `pm2 t delete <target>` | Delete a task |

`pm2 t` aliases the task namespace. Only `pm2 apply` is a standalone task
action alias registered at the root.

---

### `pm2 task stop`

Stop a task by name or stop all.

```bash
pm2 task stop <name>
pm2 task stop all
```

Sends `SIGTERM`; escalates to `SIGKILL` after 5 seconds. A deliberately stopped process is never auto-restarted.

The daemon itself keeps running — to stop it, use `pm2 daemon kill` (see below).

---

### `pm2 daemon` / `pm2 d` — manage the daemon

The daemon is the long-running process that owns the socket, the registry, and the cron scheduler:

```bash
pm2 start                  # short form of pm2 daemon start
pm2 d start                # namespace short alias
pm2 daemon start           # spawn the daemon (background by default)
pm2 daemon start --foreground   # run in foreground (blocking; Ctrl+C stops it)
pm2 daemon kill            # gracefully stop every process, then exit the daemon
```

Bare `pm2 daemon` (no subcommand) errors out — pick a verb. The internal auto-start paths (`pm2 task start` on a fresh install) call `pm2 daemon start --foreground` via `exec`, so the verb is always present in argv.

#### `stop` vs `daemon kill` — which one do I want?

These two look similar but operate on different layers of the system:

| Aspect | `pm2 task stop <name\|id\|all>` | `pm2 daemon kill` |
| ------ | -------------------------- | ----------------- |
| Operates on | a managed process | the daemon itself |
| Daemon afterwards | still running, still accepting RPC | exited (process count drops to zero) |
| Signal path | `executor.Stop` → SIGTERM → 5 s → SIGKILL (same path) | same path, applied to every mp, then `os.Exit(0)` |
| Restartability | re-launchable with `pm2 task start` | requires `pm2 daemon start` to bring it back |
| When the daemon is unreachable | error: cannot dial socket | idempotent: prints "PM2 daemon is not running." and returns nil |

> **Removed in this revision:** the legacy top-level `pm2 kill` command has been deleted. It was always equivalent to `pm2 daemon kill` plus a `Deprecated:` marker; the canonical entry point is now exclusively under the `daemon` group. Scripts calling `pm2 kill` will see `Error: unknown command "kill" for "pm2"`.

---

### `pm2 task restart`

Stop then immediately re-launch, preserving all config including `cron_restart`.

```bash
pm2 task restart <name>
pm2 task restart all
```

---

### `pm2 task delete`

Stop and remove from the process list.

```bash
pm2 task delete <name>
pm2 task delete all
```

Does not affect `~/.pm2/dump.json`.

---

### `pm2 list` / `pm2 l` / `pm2 ls` / `pm2 status`

Print one non-interactive process snapshot using the bordered, status-coloured
table formerly shown by the wide `pm2 m` view.

```bash
pm2 list
pm2 l                 # short alias
pm2 list --no-color   # plain output for logs and pipelines
```

The table keeps runtime columns such as ID, namespace, PID, uptime, restart
count, status, CPU, and memory. Optional metadata columns are removed on narrow
terminals.

---

### `pm2 logs`

Continuously stream managed application logs. The optional target may be an
application name, ID, namespace, or `namespace:name`; without a target, all
managed applications are followed.

```bash
pm2 logs
pm2 logs api
pm2 logs production:api
```

Each emitted line uses this format:

```text
[YYYY-MM-DD HH:MM:SS] app_name | escaped_log
```

stdout log entries go to command stdout and stderr log entries go to command
stderr. Streaming starts at each current file's end, follows file replacement
or truncation, and continues until `Ctrl+C`. Managed output uses visible
Go-style escapes: for example newline is `\n`, tab is `\t`, and ANSI escape is
`\x1b`. PM2 owns the physical newline that separates stored log records.

External Go services can consume the same typed stream through
`logfile.Follow(ctx, sources)`, which returns receive-only `Entry` and error
channels. Each `Entry` includes `Time`, `AppName`, `Stream`, and `Message`;
`Entry.String()` produces the terminal format above.

Managed stdout and stderr lines use a `[YYYY-MM-DD HH:MM:SS] ` prefix. The
current `daemon.log` / `daemon.err` files keep the latest date; older leading
date blocks move to `daemon.<YYYY-MM-DD>.log` and
`daemon.<YYYY-MM-DD>.err`. Defaults live under
`~/.config/<app_name>/logs/`; custom `log_file`, `out_file`, and `error_file`
paths retain their own basename and directory.

---

### `pm2 logs monitor` / `pm2 logs m`

Open the interactive managed-log split view. The optional target selects the
initial application row.

```bash
pm2 logs monitor
pm2 logs m api
pm2 logs m production:api
```

```text
Tree Explorer (left) │ Log Viewer (right)
```

- Application rows begin with `[<id>]`; current log files use the `🔶` marker.
- With Tree focus, `↑` / `↓` or `j` / `k` moves through application/file rows.
- `→`: expand an application or load and focus its selected log file.
- `Enter`: load the selected Tree file and focus the right-hand Viewer.
- With Viewer focus, `↑` / `↓` or `j` / `k` moves through log lines.
- `←`: return focus to the Tree, or collapse the selected Tree branch.
- `PageUp` / `PageDown`: move one visible page in the Log Viewer.
- `d`: on a Tree file row, request deletion; only explicit `y` deletes it
  (`n` / `Esc` cancels).
- `q`: quit.

The loaded log remains visible when focus returns to the Tree. Deletion is
intentionally unavailable while the Log Viewer has focus.

---

### `pm2 monitor` / `pm2 m`

Open the interactive two-pane process detail and log dashboard. Refreshes every
2 seconds. `pm2 m` opens this view directly; there is no `-d` / `--detail`
flag.

```bash
pm2 m
```

```text
pm2 monitor  4 processes · 10:24:51
──────────────────────┬────────────────────────────────────────
 PROCESSES            │ DETAIL — api
                      │
 ● api         3d2h   │ script    /home/user/myapp/bin/server
 ● worker-0    1d4h   │ cwd       /home/user/myapp
 ◌ worker-1    0s     │ status    online
 ○ nightly     —      │ uptime    3 days  14:22:11
                      │ started   2026-06-09  19:31:04
                      │ restarts  0 / 15 max
                      │ cron      0 3 * * *  →  next 06-13 03:00
                      │ stdout    ~/.pm2/logs/api-out.log
                      │ stderr    ~/.pm2/logs/api-err.log
                      ├────────────────────────────────────────
                      │ LOGS — api
                      │ [2026-06-12 10:00:01] server listening on :8080
                      │ [2026-06-12 10:24:51] GET /api/health 200 4ms
──────────────────────┴────────────────────────────────────────
 ↑↓/jk navigate  │  r restart  │  s stop  │  d delete  │  q quit
```

---

### `pm2 taskmanager` (alias: `pm2 tm`)

Open the system activity monitor. Where `pm2 monitor` answers "what are my
managed applications doing", `pm2 taskmanager` answers "what is this machine
doing, and which part of it is mine".

The top panel is whole-machine: CPU (with user/system split and load
average), memory (used, available and swap), network throughput, and disk
I/O with filesystem capacity. Below it, a selectable list of pm2 tasks —
or, with `a`, every process on the machine. The right pane breaks the
selection down into the sub-processes it spawned and the ports its whole
tree listens on.

The daemon is optional: without one the machine panel and the process list
still work, and only the task list is empty.

```bash
pm2 taskmanager # short alias: pm2 tm
```

```text
 pm2 taskmanager  workstation · 10 cores · up 12h 6m · 737 procs (3 running) · 16:16:30
 cpu   ███████░░░░░░░░░░░  38.0%  user 15.0  sys 23.0  ·  load 4.25 6.80 8.22  ·  10 cores
 mem   ██████████████████  98.7%  15.8gb of 16.0gb  ·  3.1gb available  ·  swap 10.7gb / 12.0gb
 net                      ⇣ 1.2mb/s   ⇡ 0.4mb/s  on en0  ·  2.9gb in / 1.0gb out since boot
 disk                     ⇅ 43.3mb/s  4411 io/s  ·  / 11.7gb/228.3gb 5%
 PM2 TASKS (18)                       │ DETAIL — SERVICE:API
 ● api                  20.0%   4.0mb │ status           online
 ● worker               12.1%  88.2mb │ pid              1978
 ⏸ nightly               0.0%      0b │ uptime           3 days  14:22:11
                                      │ cpu              2.0%   tree 20.0%
                                      │ memory           1.0mb   tree 4.0mb
                                      │ SUB-PROCESSES (1)
                                      │   2001   18.0%     3.0mb  /bin/worker
                                      │ LISTENING PORTS (1)
                                      │ tcp   0.0.0.0:8080            pid 2001
 ↑↓ / jk navigate  │  a all processes  │  s sort: cpu  │  q quit
```

Keys: `↑↓` / `jk` navigate, `PgUp` / `PgDn` page, `g` / `G` jump to
top / bottom, `a` toggle between pm2 tasks and every OS process, `s` cycle
the sort order (cpu → memory → name), `q` quit.

On macOS, memory "used" follows the platform's own definition — everything
that is not free or speculative — so a healthy Mac reads near 99%. The
available figure beside it is the headroom that actually exists.

---

### `pm2 taskmanager emit`

Write one complete detection per interval instead of drawing it: host
resources, filesystem usage, and every managed task with its sub-processes
and listening ports. Each record is self-contained, so a consumer that
misses one loses a sample rather than falling out of sync.

```bash
pm2 taskmanager emit --interval 30s                       # NDJSON on stdout
pm2 taskmanager emit --interval 1m --format text --out ~/.config/pm2/logs/dashboard.log
pm2 taskmanager emit --count 1 | jq '.system.cpu'         # one-shot probe
```

| Flag         | Default | Meaning                                            |
| ------------ | ------- | -------------------------------------------------- |
| `--interval` | `30s`   | Period between snapshots                           |
| `--count`    | `0`     | Stop after N snapshots; 0 runs until interrupted   |
| `--out`      | stdout  | Append to this file instead of stdout              |
| `--format`   | `json`  | `json` (newline-delimited) or `text` (key=value)   |

The first snapshot is written immediately, then one per interval. `json`
emits one object per line for `jq` or a log shipper; `text` emits
`[YYYY-MM-DD HH:MM:SS] scope=… key=value` lines matching the managed-log
prefix, so it reads naturally when tailed alongside application logs.

The daemon is never auto-started: a task list that cannot be read is
reported in the snapshot's `errors` field and the machine readings still
ship.

---

### `pm2 save` / `pm2 s`

Persist the current process list to `~/.pm2/dump.json`.

```bash
pm2 save
pm2 s
```

Every task operation saves automatically — `start` / `apply`, `restart`,
`stop`, `pause`, `resume`, `delete`, including `pm2 apply --delete`. `dump.json`
therefore already matches the last thing you did, so a daemon restart right
afterwards will not resurrect a deleted task, forget a newly added one, or
undo a pause. Running `pm2 save` by hand is now only needed to force a
checkpoint; the periodic auto-save (default every 10 minutes,
`PM2_AUTO_SAVE_INTERVAL`) remains as a backstop.

Cron fires and file-watch restarts deliberately do not trigger a save: they
are not operations you issued and they change nothing that is persisted.

---

### `pm2 resurrect` / `pm2 r`

Restore the last saved process list from `~/.pm2/dump.json`.

```bash
pm2 resurrect
pm2 r
```

---

### `pm2 startup`

Generate an OS startup script so the daemon launches on login/boot.

```bash
pm2 startup
```

- macOS → `~/Library/LaunchAgents/com.shuk.pm2.plist`
- Linux → `~/.config/systemd/user/pm2.service`

Activate (macOS):

```bash
launchctl load ~/Library/LaunchAgents/com.shuk.pm2.plist
pm2 save
```

---

## Ecosystem config

Two formats are supported. Relative `script` paths resolve relative to the config file's directory.

### `ecosystem.config.json` (recommended)

```json
{
    "apps": [
        {
            "name": "api",
            "script": "./bin/server",
            "args": ["--port", "8080"],
            "instances": 2,
            "env": {
                "NODE_ENV": "production",
                "PORT": "8080"
            },
            "cron_restart": "0 3 * * *",
            "max_restarts": 10,
            "log_file": "/var/log/api-out.log",
            "error_file": "/var/log/api-err.log"
        }
    ]
}
```

### `ecosystem.config.js`

Parsed via an embedded JS runtime (ES2015+). No `require()` or Node.js built-ins.

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

### Config fields

| Field          | Type     | Default                       | Description                                    |
| -------------- | -------- | ----------------------------- | ---------------------------------------------- |
| `namespace`    | string   | `"default"`                   | Process namespace                              |
| `name`         | string   | script filename               | Process identifier — must be unique            |
| `script`       | string   | required                      | Executable path                                |
| `args`         | []string | `[]`                          | Arguments forwarded to the process             |
| `instances`    | int      | `1`                           | Parallel copies (`<name>-0`, `<name>-1`, …)    |
| `env`          | map      | `{}`                          | Env vars merged with the inherited environment |
| `cron_restart` | string   | `""`                          | 5-field cron expression for scheduled restart  |
| `cron`         | string   | `""`                          | 5-field cron expression to trigger execution   |
| `watch`        | bool     | `false`                       | Watch file changes to restart                  |
| `max_restarts` | int      | `15`                          | Crash auto-restart ceiling                     |
| `log_file`     | string   | `~/.pm2/logs/<name>-out.log`  | stdout path                                    |
| `out_file`     | string   | `""`                          | Alias for stdout path                          |
| `error_file`   | string   | `~/.pm2/logs/<name>-err.log`  | stderr path                                    |
| `config_dir`   | string   | `"~/.config/<name>/"`         | Base directory for log files                   |
| `config_file`  | string   | `"<cwd>/ecosystem.config.js"` | Path to ecosystem config file (auto-set)       |
| `cwd`          | string   | ecosystem file directory      | Working directory used to run the process      |
| `optional`     | bool     | `false`                       | Register paused unless selected by run flags   |

---

## Auto-restart behaviour

| Exit condition                 | Result                                       |
| ------------------------------ | -------------------------------------------- |
| Non-zero exit code (`errored`) | Auto-restart after 1 s, up to `max_restarts` |
| Zero exit code (`stopped`)     | No restart — treated as intentional          |
| `pm2 task stop` (any exit code)     | No restart — `stopping` flag suppresses it   |
| `cron_restart` fires           | Forced restart regardless of current status  |

---

## Cron expression format

```bash
┌───── minute    (0–59)
│ ┌─── hour      (0–23)
│ │ ┌─ day       (1–31)
│ │ │ ┌ month    (1–12)
│ │ │ │ ┌ weekday (0–6, Sunday = 0)
│ │ │ │ │
* * * * *
```

| Expression    | Meaning               |
| ------------- | --------------------- |
| `*/5 * * * *` | Every 5 minutes       |
| `0 * * * *`   | Every hour            |
| `0 0 * * *`   | Daily at midnight     |
| `0 2 * * 0`   | Every Sunday at 02:00 |

---

## Typical workflow

```bash
# 1. Write config
cat > ecosystem.config.json << 'EOF'
{
  "apps": [{ "name": "api", "script": "./bin/server",
             "env": {"PORT": "8080"}, "cron_restart": "0 3 * * *" }]
}
EOF

# 2. Run tasks
pm2 task start ecosystem.config.json

# 3. Monitor
pm2 monitor

# 4. Deploy new build
pm2 task restart api

# 5. Persist + enable on boot
pm2 save
pm2 startup
launchctl load ~/Library/LaunchAgents/com.shuk.pm2.plist   # macOS
```

---

## State files

```tree
~/.pm2/
├── pm2.sock            Unix socket — CLI ↔ daemon RPC
├── dump.json           saved process list (pm2 save / resurrect)
└── logs/
    └── ...              fallback logs when an app has no config directory

~/.config/<app_name>/logs/
├── daemon.log                  current stdout
├── daemon.err                  current stderr
├── daemon.<YYYY-MM-DD>.log     rotated stdout
└── daemon.<YYYY-MM-DD>.err     rotated stderr
```

---

## License

This project is licensed under the GPLv3 License - see the [LICENSE](LICENSE) file for details.
