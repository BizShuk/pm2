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

Ecosystem files (`ecosystem.config.js` or `ecosystem.config.json`) let you define multiple applications at once.

Example configuration in Javascript:

```javascript
module.exports = {
    apps: [
        {
            name: "api",
            script: "./server.js",
            instances: 3,
            env: {
                PORT: "8080",
                NODE_ENV: "production"
            }
        },
        {
            name: "worker",
            script: "./worker.js",
            cron_restart: "0 * * * *"
        }
    ]
};
```

## Common Mistakes

- Confusing `pm2 stop` with `pm2 daemon kill` or `pm2 daemon stop`. Always double check if you want to stop a single process or the entire daemon.
- Expecting a cron task to stop firing with `pm2 stop`. Use `pm2 pause` to suspend cron schedules.
- Starting a script with a duplicate name but different code. Use `pm2 delete` first to remove the old entry.
