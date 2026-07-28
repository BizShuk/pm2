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

State directory: `~/.pm2/`

```tree
~/.pm2/
├── pm2.sock        # Unix socket for CLI ↔ daemon RPC
├── dump.json       # saved process list (pm2 save)
└── logs/
    ├── <name>-out.log
    └── <name>-err.log
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
| `log_file`     | string            | `~/.pm2/logs/<name>-out.log` | stdout log path                                             |
| `error_file`   | string            | `~/.pm2/logs/<name>-err.log` | stderr log path                                             |

---

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
in the dashboard until started again. Does not affect `dump.json`.

### `pm2 logs`

```bash
pm2 logs              # tail all processes
pm2 logs api-server   # tail one process
pm2 logs -n 50        # show last 50 lines instead of default 20
```

Prints the last N lines from stdout + stderr log files, then follows the
first matching log in real time. Press `Ctrl+C` to exit.

### `pm2 save` / `pm2 s`

```bash
pm2 save
pm2 s
```

Persists the current process list to `~/.pm2/dump.json`, including all fields
needed to restore processes exactly (`cron_restart`, `env`, `args`, etc.).

### `pm2 resurrect` / `pm2 r`

```bash
pm2 resurrect
pm2 r
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

# 5. Watch logs
pm2 logs api

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
