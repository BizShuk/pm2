# pm2

A PM2-inspired process manager written in Go. Manages long-running processes with automatic restart, cron-based scheduling, a live TUI dashboard, and OS startup integration.

## Install

```bash
git clone https://github.com/bizshuk/pm2
cd pm2
go build -o /usr/local/bin/pm2 .
```

State directory: `~/.pm2/` (created automatically on first run)

---

## Commands

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

### `pm2 start`

Start a process from a script path or an ecosystem config file.

```bash
pm2 start <script|ecosystem.config.js|ecosystem.config.json> [flags]

Flags:
  -n, --name string           process name (default: script filename)
  -i, --instances int         number of parallel instances
      --cron-restart string   cron schedule for automatic restart
  -e, --env stringArray       environment variable  KEY=VAL  (repeatable)
```

Examples:

```bash
# Bare binary
pm2 start /usr/local/bin/myserver

# Named, with env vars
pm2 start /usr/local/bin/myserver --name api --env PORT=8080 --env ENV=prod

# 3 parallel workers
pm2 start ./worker --name worker -i 3

# Cron-based restart every hour
pm2 start ./server --name api --cron-restart "0 * * * *"

# Pass extra args to the process
pm2 start /bin/sleep 3600

# Ecosystem config (JSON or JS)
pm2 start ./ecosystem.config.json
pm2 start ./ecosystem.config.js
```

Process identity is `name + script path`. Re-starting with the same name and script replaces the existing process. Re-starting with the same name but a different script returns an error — use `pm2 delete` first.

---

### `pm2 stop`

Stop a process by name or stop all.

```bash
pm2 stop <name>
pm2 stop all
```

Sends `SIGTERM`; escalates to `SIGKILL` after 5 seconds. A deliberately stopped process is never auto-restarted.

The daemon itself keeps running — to stop it, use `pm2 daemon kill` (see below).

---

### `pm2 daemon` — manage the daemon

The daemon is the long-running process that owns the socket, the registry, and the cron scheduler. Lifecycle is a two-verb command group:

```bash
pm2 daemon start           # spawn the daemon (background by default)
pm2 daemon start --foreground   # run in foreground (blocking; Ctrl+C stops it)
pm2 daemon kill            # gracefully stop every process, then exit the daemon
```

Bare `pm2 daemon` (no subcommand) errors out — pick a verb. The internal auto-start paths (`pm2 start` on a fresh install) call `pm2 daemon start --foreground` via `exec`, so the verb is always present in argv.

#### `stop` vs `daemon kill` — which one do I want?

These two look similar but operate on different layers of the system:

| Aspect | `pm2 stop <name\|id\|all>` | `pm2 daemon kill` |
| ------ | -------------------------- | ----------------- |
| Operates on | a managed process | the daemon itself |
| Daemon afterwards | still running, still accepting RPC | exited (process count drops to zero) |
| Signal path | `executor.Stop` → SIGTERM → 5 s → SIGKILL (same path) | same path, applied to every mp, then `os.Exit(0)` |
| Restartability | re-launchable with `pm2 start` | requires `pm2 daemon start` to bring it back |
| When the daemon is unreachable | error: cannot dial socket | idempotent: prints "PM2 daemon is not running." and returns nil |

> **Removed in this revision:** the legacy top-level `pm2 kill` command has been deleted. It was always equivalent to `pm2 daemon kill` plus a `Deprecated:` marker; the canonical entry point is now exclusively under the `daemon` group. Scripts calling `pm2 kill` will see `Error: unknown command "kill" for "pm2"`.

---

### `pm2 restart`

Stop then immediately re-launch, preserving all config including `cron_restart`.

```bash
pm2 restart <name>
pm2 restart all
```

---

### `pm2 delete` / `pm2 del`

Stop and remove from the process list.

```bash
pm2 delete <name>
pm2 delete all
```

Does not affect `~/.pm2/dump.json`.

---

### `pm2 list` / `pm2 ls` / `pm2 status`

Print one non-interactive process snapshot using the bordered, status-coloured
table formerly shown by the wide `pm2 m` view.

```bash
pm2 list
pm2 list --no-color   # plain output for logs and pipelines
```

The table keeps runtime columns such as ID, namespace, PID, uptime, restart
count, status, CPU, and memory. Optional metadata columns are removed on narrow
terminals.

---

### `pm2 logs`

Tail stdout and stderr log files for a process.

```bash
pm2 logs [name] [flags]

Flags:
  -n, --lines int   number of lines to show (default 20)
```

```bash
pm2 logs             # tail all processes
pm2 logs api         # tail one process
pm2 logs api -n 50   # show last 50 lines
```

Log files are stored in `~/.pm2/logs/<name>-out.log` and `~/.pm2/logs/<name>-err.log` unless overridden in the config.

---

### `pm2 monitor` / `pm2 m` / `pm2 dashboard`

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
 ● worker-0    1d4h   │ status    online
 ◌ worker-1    0s     │ uptime    3 days  14:22:11
 ○ nightly     —      │ started   2026-06-09  19:31:04
                      │ restarts  0 / 15 max
                      │ cron      0 3 * * *  →  next 06-13 03:00
                      │ stdout    ~/.pm2/logs/api-out.log
                      │ stderr    ~/.pm2/logs/api-err.log
                      ├────────────────────────────────────────
                      │ LOGS — api
                      │ 10:00:01 server listening on :8080
                      │ 10:24:51 GET /api/health 200 4ms
──────────────────────┴────────────────────────────────────────
 ↑↓/jk navigate  │  r restart  │  s stop  │  d delete  │  q quit
```

---

### `pm2 save`

Persist the current process list to `~/.pm2/dump.json`.

```bash
pm2 save
```

---

### `pm2 resurrect`

Restore the last saved process list from `~/.pm2/dump.json`.

```bash
pm2 resurrect
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

---

## Auto-restart behaviour

| Exit condition                 | Result                                       |
| ------------------------------ | -------------------------------------------- |
| Non-zero exit code (`errored`) | Auto-restart after 1 s, up to `max_restarts` |
| Zero exit code (`stopped`)     | No restart — treated as intentional          |
| `pm2 stop` (any exit code)     | No restart — `stopping` flag suppresses it   |
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

# 2. Start
pm2 start ecosystem.config.json

# 3. Monitor
pm2 monitor

# 4. Deploy new build
pm2 restart api

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
    ├── <name>-out.log  stdout
    └── <name>-err.log  stderr
```

---

## License

This project is licensed under the GPLv3 License - see the [LICENSE](LICENSE) file for details.
