---
name: pm2
description: Use when running, stopping, restarting, saving, restoring, pausing, or monitoring background processes, cron schedules, or ecosystem files via the pm2 command-line tool.
---

# pm2 Commands

## Overview

A reference guide for managing background processes, daemon lifecycle, and application configurations using the `pm2` command-line utility.

## When to Use

- Spawning, stopping, killing, or checking the status of the `pm2` daemon.
- Registering, starting, stopping, restarting, pausing, resuming, or deleting processes.
- Tailing or viewing application logs.
- Saving active configurations or resurrecting them on boot.
- Designing ecosystem files interactively or installing planner agents.

When NOT to use:

- Modifying the internal Go codebase of the daemon or executor.

## Command Reference

| Command                       | Purpose                                      | Usage / Key Flags                                                                        |
| ----------------------------- | -------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `pm2 start <target>`          | Start process from script/ecosystem config   | `-n, --name string`, `-i, --instances int`, `--cron-restart "expr"`, `-e, --env KEY=VAL` |
| `pm2 stop <name\|id\|all>`    | Stop a process gracefully                    | `SIGTERM` escalated to `SIGKILL` after 5 seconds                                         |
| `pm2 restart <name\|id\|all>` | Restart a process                            | Closes, re-spawns, and re-registers scheduler                                            |
| `pm2 pause <name\|id\|all>`   | Suspend a process and its cron schedule      | Removes scheduler entries; status becomes `paused`                                       |
| `pm2 resume <name\|id\|all>`  | Resume a paused process                      | Re-registers cron and launches the process                                               |
| `pm2 delete <name\|id\|all>`  | Remove a process from the registry           | Removes configuration and stops process                                                  |
| `pm2 list`                    | Print styled non-interactive process table   | Bordered snapshot; `--no-color` for plain output                                         |
| `pm2 logs [name\|id]`         | Tail log files directly                      | `--lines N` to specify trailing lines                                                    |
| `pm2 save`                    | Persist current app configs                  | Saves to `~/.pm2/dump.json`                                                              |
| `pm2 resurrect`               | Restore saved app configs                    | Loads from `~/.pm2/dump.json`                                                            |
| `pm2 monitor` / `pm2 m`       | Launch Bubbletea terminal dashboard          | Opens the two-pane process detail/log view directly; no `-d` flag                        |
| `pm2 startup`                 | Generate OS boot startup scripts             | Creates `plist` on macOS, systemd unit on Linux                                          |
| `pm2 daemon start`            | Spawn the daemon process                     | `--foreground` to run blocking in foreground                                             |
| `pm2 daemon kill`             | Gracefully exit all apps and daemon          | CLI commands can still auto-start the daemon                                             |
| `pm2 daemon stop`             | Shutdown all apps, daemon & block auto-start | Writes a stop marker to suppress auto-respawn                                            |
| `pm2 daemon status`           | Read-only daemon status check                | Works whether the daemon is running or not                                               |
| `pm2 wizard`                  | Interactively build ecosystem config         | `-o, --output`, `-f, --force`, `-y, --yes` (accept all defaults)                         |
| `pm2 wizard install <scr>`    | Register pre-configured planner agent        | `--system-planner` or `--business-planner`                                               |

## Key Differences

### `pm2 stop` vs `pm2 daemon kill` vs `pm2 daemon stop`

- `pm2 stop` terminates a single managed process. The daemon keeps running.
- `pm2 daemon kill` terminates all managed processes, then gracefully exits the daemon itself. Subsequent CLI commands can still auto-respawn it.
- `pm2 daemon stop` terminates all managed processes, exits the daemon, and writes a stop marker that blocks silent auto-spawning from other CLI commands. Use `pm2 daemon start` to clear the marker and allow auto-spawning again.

### `pm2 pause` vs `pm2 stop`

- For cron-triggered tasks, `pm2 stop` leaves the scheduler registered, meaning the cron task will still fire at its next scheduled time.
- `pm2 pause` removes the task from the cron scheduler and marks its state as `paused`. The process will not run again until `pm2 resume` is executed.

## Ecosystem Configurations

Ecosystem files (`ecosystem.config.js` or `ecosystem.config.json`) let you define multiple applications at once. Place one at the repo root; relative `script` paths resolve against the config file's directory.

See [references/ecosystem.config.js](references/ecosystem.config.js) for the full annotated reference covering all supported fields and patterns.

### Supported AppConfig Fields

| Field          | Type          | Default                       | Description                                          |
| -------------- | ------------- | ----------------------------- | ---------------------------------------------------- |
| `name`         | string        | script filename               | Process name shown in `pm2 list`                     |
| `namespace`    | string        | `"default"`                   | Group label for organising processes                  |
| `script`       | string        | —                             | Path or `$PATH`-resolvable command (required)         |
| `args`         | string[]      | `[]`                          | Arguments passed to the script                       |
| `instances`    | int           | `1`                           | Number of process copies to spawn                    |
| `env`          | object        | `{}`                          | Environment variables as key-value pairs             |
| `cron`         | string        | `""`                          | 5-field cron expression — one-shot scheduled task    |
| `cron_restart` | string        | `""`                          | 5-field cron expression — restarts a running process |
| `watch`        | bool          | `false`                       | Restart on file changes via fsnotify                 |
| `autorestart`  | bool          | `true`                        | Restart on crash; set `false` for one-shot tasks     |
| `max_restarts` | int           | `15`                          | Crash-restart ceiling before giving up               |
| `cwd`          | string        | config file dir               | Working directory for the spawned process            |
| `out_file`     | string        | `~/.config/<name>/logs/...`   | Custom stdout log path                               |
| `error_file`   | string        | `~/.config/<name>/logs/...`   | Custom stderr log path                               |
| `config_dir`   | string        | `~/.config/<name>/`           | Override the config/log root directory               |

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

Runs once at 06:00 daily, exits, waits for the next schedule. Use `pm2 pause` to suspend, `pm2 resume` to reactivate.

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
    autorestart: false,
    watch: false
}
```

`__dirname` is available in `.js` configs (goja runtime). `autorestart: false` prevents crash-restarts between cron fires.

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
    ]
}
```

Multiple apps in one config file. Each gets its own name, schedule, and lifecycle.

## Workflow Examples

### Start all apps from ecosystem file

```bash
pm2 start ecosystem.config.js
```

### Start a single script with inline options

```bash
pm2 start ./worker.js -n "bg-worker" --cron-restart "0 */2 * * *" -e LOG_LEVEL=debug
```

### Suspend a cron task temporarily

```bash
pm2 pause "Disk Analysis Daily"
# ... later ...
pm2 resume "Disk Analysis Daily"
```

### Check what is running

```bash
pm2 list              # one-shot table
pm2 monitor           # interactive TUI dashboard
```

### Persist and restore across reboots

```bash
pm2 save              # write ~/.pm2/dump.json
pm2 startup           # generate launchd/systemd service
# After reboot:
pm2 resurrect         # reload saved processes
```

### Tail logs for a specific process

```bash
pm2 logs "LLM Proxy"            # follow mode
pm2 logs "LLM Proxy" --lines 50 # last 50 lines
```

### Completely shut down

```bash
pm2 daemon kill        # stop all + exit daemon (auto-respawn OK)
pm2 daemon stop        # stop all + exit daemon + block auto-respawn
```

## Common Mistakes

- Confusing `pm2 stop` with `pm2 daemon kill` or `pm2 daemon stop`. Always double check if you want to stop a single process or the entire daemon.
- Expecting a cron task to stop firing with `pm2 stop`. Use `pm2 pause` to suspend cron schedules.
- Starting a script with a duplicate name but different code. Use `pm2 delete` first to remove the old entry.
- Using `cron` when you mean `cron_restart` (or vice versa). `cron` = one-shot scheduled run; `cron_restart` = restart an already-running process on schedule.
- Setting `autorestart: true` (default) for a cron task that exits after each run — this causes an unnecessary immediate restart. Set `autorestart: false` for one-shot cron tasks.
- Forgetting that relative `script` paths resolve against the config file's directory, not the shell's CWD when running `pm2 start`.
