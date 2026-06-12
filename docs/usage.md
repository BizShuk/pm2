# pm2 — Usage Guide

A Go implementation of PM2-style process management.

---

## Installation

```bash
cd ~/projects/pm2
go build -buildvcs=false -o /usr/local/bin/pm2 .
```

The daemon starts automatically on first `pm2 start`. Alternatively start it explicitly:

```bash
pm2 daemon   # runs in foreground; use pm2 startup to daemonize on boot
```

State directory: `~/.pm2/`

```
~/.pm2/
├── pm2.sock        # Unix socket for CLI ↔ daemon RPC
├── dump.json       # saved process list (pm2 save)
└── logs/
    ├── <name>-out.log
    └── <name>-err.log
```

---

## Quick-start: single process

```bash
# Start a binary directly
pm2 start /usr/local/bin/myserver

# With a name, args, and env vars
pm2 start /usr/local/bin/myserver --name api-server --env PORT=8080 --env ENV=prod

# Multiple instances
pm2 start /usr/local/bin/myserver --name worker -i 3

# With cron-based auto-restart (every hour on the hour)
pm2 start /usr/local/bin/myserver --name api --cron-restart "0 * * * *"

# Pass extra arguments to the process after --
pm2 start /usr/bin/node --name web 300
#           ^script       ^extra arg forwarded to process
```

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
      "max_restarts": 10,
      "log_file": "/var/log/api-out.log",
      "error_file": "/var/log/api-err.log"
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
pm2 start /path/to/ecosystem.config.json
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
      script: "./bin/server",    // relative to this config file
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
}
```

```bash
pm2 start /path/to/ecosystem.config.js
```

### All `AppConfig` fields

| Field | Type | Default | Description |
|---|---|---|---|
| `name` | string | derived from script filename | Process identifier (must be unique) |
| `script` | string | required | Executable path (absolute or relative to config file) |
| `args` | []string | `[]` | Arguments forwarded to the process |
| `instances` | int | `1` | How many copies to launch (named `<name>-0`, `<name>-1`, …) |
| `env` | map[string]string | `{}` | Environment variables merged with inherited env |
| `cron_restart` | string | `""` | 5-field cron expression for scheduled restart |
| `max_restarts` | int | `15` | Auto-restart limit before giving up |
| `log_file` | string | `~/.pm2/logs/<name>-out.log` | stdout log path |
| `error_file` | string | `~/.pm2/logs/<name>-err.log` | stderr log path |

---

## Full CLI reference

### `pm2 start`

```bash
pm2 start <script|ecosystem.config.json|ecosystem.config.js> [flags] [-- extra args]

Flags:
  -n, --name string           Process name (single-script mode only)
  -i, --instances int         Parallel instance count
      --cron-restart string   Cron expression for scheduled restart
  -e, --env stringArray       Env var KEY=VAL (repeatable)
```

Examples:

```bash
# Bare script
pm2 start /usr/bin/python3 --name pyworker

# With args passed through to the script
pm2 start /bin/sleep 3600

# Ecosystem file (both formats)
pm2 start ~/myapp/ecosystem.config.json
pm2 start ~/myapp/ecosystem.config.js

# Cron restart every day at midnight
pm2 start /usr/local/bin/myapp --name myapp --cron-restart "0 0 * * *"

# 3 parallel workers with env
pm2 start ./worker -i 3 --name worker --env QUEUE=jobs --env DB_URL=postgres://...
```

### `pm2 stop`

```bash
pm2 stop <name>      # stop by name
pm2 stop all         # stop every process
```

Sends `SIGTERM`; if the process does not exit within 5 seconds, sends `SIGKILL`.
A deliberately stopped process is NOT auto-restarted even if `max_restarts > 0`.

### `pm2 restart`

```bash
pm2 restart <name>
pm2 restart all
```

Performs a clean stop then immediately re-launches with the same config.
The `cron_restart` schedule is re-registered for the new process.

### `pm2 delete` / `pm2 del`

```bash
pm2 delete <name>
pm2 delete all
```

Stops the process and removes it from the in-memory list. It will not appear
in the dashboard until started again. Does not affect `dump.json`.

### `pm2 logs`

```bash
pm2 logs              # tail all processes
pm2 logs api-server   # tail one process
pm2 logs -n 50        # show last 50 lines instead of default 20
```

Prints the last N lines from stdout + stderr log files, then follows the
first matching log in real time. Press `Ctrl+C` to exit.

### `pm2 save`

```bash
pm2 save
```

Persists the current process list to `~/.pm2/dump.json`, including all fields
needed to restore processes exactly (`cron_restart`, `env`, `args`, etc.).

### `pm2 resurrect`

```bash
pm2 resurrect
```

Reads `~/.pm2/dump.json` and starts every entry. Use this after a reboot to
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

```bash
pm2 start ./server --name api   # registers "api" (script=./server)
pm2 start ./server --name api   # same name + same script → stop old, start new ✓
pm2 start ./other  --name api   # same name, DIFFERENT script → error ✗
# Error: process "api" already exists with script "./server";
#        use 'pm2 delete api' first or use a different name
```

To replace a process with a different binary, delete it first:

```bash
pm2 delete api
pm2 start ./other --name api
```

When starting an ecosystem file with `instances > 1`, instances are named
`<name>-0`, `<name>-1`, … and each is independently identified:

```bash
pm2 stop worker-0    # stops only the first worker instance
pm2 restart worker-1 # restarts only the second
pm2 stop all         # stops every process
```

Re-running `pm2 start ecosystem.config.json` when the apps are already running
will stop-and-replace each entry by name.

---

## Relative paths in ecosystem.config

Relative `script` paths are resolved relative to the config file's directory,
not the shell's current working directory:

```
/home/user/myapp/
├── ecosystem.config.json   ← script: "./bin/server"
└── bin/
    └── server              ← resolved to /home/user/myapp/bin/server
```

```bash
# Works from any directory:
pm2 start /home/user/myapp/ecosystem.config.json
cd /tmp && pm2 start /home/user/myapp/ecosystem.config.json  # same result
```

> Paths are resolved at parse time in the CLI process before being sent to
> the daemon as absolute paths. The daemon always receives absolute paths.

---

## How failed/exited processes are monitored

Each launched process has a dedicated `watchProcess` goroutine that calls
`cmd.Wait()` — this blocks until the OS process exits. No polling is used.

```
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
                                                   ├── YES → sleep 1s → re-launch
                                                   └── NO  → Status: errored (final)
```

Key rules:

- Zero exit code → `stopped` — treated as intentional, never auto-restarted
- Non-zero exit code → `errored` → auto-restarted with 1 second delay
- `pm2 stop` sets a `stopping` flag before sending SIGTERM — even though the
  process exits non-zero (killed), the flag suppresses auto-restart
- Counter `restarts` accumulates across the life of the entry (not reset on
  `pm2 restart`); `max_restarts` default is 15

---

## How `cron_restart` works

`cron_restart` schedules a forced restart at a given time, independent of
whether the process is healthy or crashed.

```
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

```
┌───── minute (0–59)
│ ┌─── hour (0–23)
│ │ ┌─ day of month (1–31)
│ │ │ ┌ month (1–12)
│ │ │ │ ┌ day of week (0–6, Sunday=0)
│ │ │ │ │
* * * * *
```

Common examples:

| Expression | Meaning |
|---|---|
| `*/5 * * * *` | Every 5 minutes |
| `0 * * * *` | Every hour on the hour |
| `0 0 * * *` | Every day at midnight |
| `0 2 * * 0` | Every Sunday at 02:00 |
| `30 6 1 * *` | 1st of every month at 06:30 |

> The cron entry is removed when the process is stopped or deleted, and
> re-registered when it is restarted. So `pm2 stop` cancels the schedule
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

# 3. Start
pm2 start ~/myapp/ecosystem.config.json

# 4. Check status
pm2 list

# 5. Watch logs
pm2 logs api

# 6. Save for resurrection on reboot
pm2 save

# 7. Generate and activate startup script (macOS)
pm2 startup
launchctl load ~/Library/LaunchAgents/com.shuk.pm2.plist

# 8. Rolling restart after a deploy
pm2 restart api

# 9. Teardown
pm2 delete all
```
