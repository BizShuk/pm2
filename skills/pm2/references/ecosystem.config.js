// ecosystem.config.js — Comprehensive reference for pm2
//
// Place this file at the repo root. pm2 resolves relative `script`
// paths against the directory of this config file, not the CWD.
//
// Supported formats: .js (module.exports = {...}) and .json.
// The .js format is preferred because it supports comments.
//
// The .js file is evaluated by goja, NOT by Node. There is no `require`,
// no `__dirname`, and no `process` — paths must be written literally.
// Relative paths are resolved against this file's directory anyway, so
// __dirname was never needed.
//
// ─── AppConfig fields ──────────────────────────────────────────────
//
//   name          string   Process name (default: script filename)
//   namespace     string   Group label shown in `pm2 list` (default: "default")
//   script        string   Path or $PATH-resolvable command (required)
//   args          []string Arguments passed to the script
//   instances     int      Number of process copies (default: 1)
//   env           {}       Environment variables as key-value pairs
//   cron          string   5-field cron expression — one-shot scheduled task
//   cron_restart  string   5-field cron expression — restarts a running process
//   watch         bool     Restart on file changes via fsnotify (default: false)
//   max_restarts  int      Crash-restart ceiling (default: 15)
//   cwd           string   Working directory (default: ecosystem file directory)
//   optional      bool     Register paused unless selected (default: false)
//
// Runtime-managed fields version, config_file, base_env, and paused are
// intentionally excluded. This Go implementation does not support the
// Node.js PM2 `autorestart` field.
//
// Log paths are NOT configurable. Every task writes to
// ~/.config/pm2/tasks/logs/<normalised-name>.log and .err, derived from
// the task name so no user-supplied string can steer a log file out of
// the directory pm2 owns. The former config_dir / log_file / out_file /
// error_file fields were removed.
//
// ─── WorkflowConfig fields ─────────────────────────────────────────
//
//   name       string   Required — there is no filename to derive one from
//   category   string   Grouping label (default: "default")
//   stages     []Stage  At least one; run in declaration order
//   cron       string   Schedule for the whole workflow
//   env        {}       Merged into every script stage
//   cwd        string   Default working directory (default: this file's dir)
//   timeout    string   Go duration, e.g. "30m" — per-stage ceiling
//
// Each stage sets EXACTLY ONE of:
//
//   script  + args/env/cwd   an inline command
//   task    "<name>"         run a registered task's command once
//   workflow "<ref>"         run another workflow inline and wait
//
// args/env/cwd on a task or workflow stage is a load error, not a
// silently ignored field.
//
// ─── Conventions ───────────────────────────────────────────────────
//
//   - One ecosystem.config.js per repo root.
//   - Use `namespace` to group by concern: "Service", "Local", "Agent", "planner".
//   - For one-shot cron tasks: set `cron` (not `cron_restart`).
//   - For long-running daemons that restart on a schedule: set `cron_restart`.
//   - Log paths are derived from the task name; they are not configurable.
//   - A workflow stage runs exactly once. Auto-restart, cron_restart,
//     watch and instances do not apply to it.
//

module.exports = {
    apps: [
        // ──────────────────────────────────────────────────────────
        // Pattern 1: Long-running daemon (always-on service)
        // ──────────────────────────────────────────────────────────
        {
            name: "LLM Proxy",
            namespace: "Service",
            script: "proxy",
            instances: 1,
            env: {
                PORT: "8080",
                LOG_LEVEL: "info"
            }
        },

        // ──────────────────────────────────────────────────────────
        // Pattern 2: One-shot cron task (daily scan)
        // ──────────────────────────────────────────────────────────
        // `cron` fires the script once per schedule; the process
        // exits naturally after each run.
        {
            name: "Disk Analysis Daily",
            namespace: "Local",
            script: "dux",
            args: ["scan"],
            cron: "0 6 * * *"
        },

        // ──────────────────────────────────────────────────────────
        // Pattern 3: Shell script with weekly schedule
        // ──────────────────────────────────────────────────────────
        // Relative paths resolve against this config file's directory.
        {
            name: "Launch Audit",
            namespace: "Local",
            script: "./bin/mac/launch_audit-mac.sh",
            cron: "0 5 * * 5"
        },

        // ──────────────────────────────────────────────────────────
        // Pattern 4: AI agent planner on a schedule
        // ──────────────────────────────────────────────────────────
        // Note the literal path: goja provides no __dirname.
        {
            name: "agy-system-planner",
            script: "agy",
            args: [
                "--add-dir",
                "/Users/me/projects/example",
                "-p",
                "run /system-planner for current workspace"
            ],
            namespace: "planner",
            instances: 1,
            cron: "10 0-9 * * *",
            watch: false
        },

        // ──────────────────────────────────────────────────────────
        // Pattern 5: CLI tool with arguments (Go binary)
        // ──────────────────────────────────────────────────────────
        // Bare command names are resolved via $PATH.
        {
            name: "Golang Clean Cache",
            namespace: "Local",
            script: "go",
            args: ["clean", "-cache"],
            cron: "0 10 * * 5"
        },

        // ──────────────────────────────────────────────────────────
        // Pattern 6: Always-on service with custom working directory
        // ──────────────────────────────────────────────────────────
        {
            name: "Ollama",
            script: "ollama",
            namespace: "Agent",
            args: ["serve"],
            instances: 1
        },

        // ──────────────────────────────────────────────────────────
        // Pattern 7: Node.js app with env + cron_restart
        // ──────────────────────────────────────────────────────────
        // `cron_restart` restarts a long-running process on schedule
        // (e.g., daily memory reset). The process stays online
        // between restarts.
        {
            name: "api-server",
            namespace: "Service",
            script: "./server.js",
            instances: 3,
            cron_restart: "0 4 * * *",
            env: {
                PORT: "3000",
                NODE_ENV: "production"
            }
        },

        // ──────────────────────────────────────────────────────────
        // Pattern 8: File watcher (restart on source changes)
        // ──────────────────────────────────────────────────────────
        {
            name: "dev-server",
            namespace: "Local",
            script: "./main.go",
            watch: true,
            env: {
                DEBUG: "true"
            }
        }
    ],

    // ─── Workflows ─────────────────────────────────────────────────
    //
    // A workflow runs its stages in order and stops at the first
    // failure. Success is exit code 0 and nothing else; a stage runs
    // exactly once and never enters `pm2 list`.
    workflows: [
        // ──────────────────────────────────────────────────────────
        // Pattern A: nightly pipeline mixing all three stage kinds
        // ──────────────────────────────────────────────────────────
        {
            name: "nightly",
            category: "ci",
            cron: "0 2 * * *",
            timeout: "30m",
            env: { CI: "1" },
            stages: [
                // inline command
                { name: "pull", script: "./scripts/pull.sh", args: ["--ff-only"] },
                // run a registered task once — no restart, no schedule
                { name: "test", task: "Disk Analysis Daily" },
                // run another workflow inline and wait for it
                { name: "ship", workflow: "ci:deploy" }
            ]
        },

        // ──────────────────────────────────────────────────────────
        // Pattern B: a workflow triggered only by webhook
        // ──────────────────────────────────────────────────────────
        // No cron: it runs when something POSTs
        //   /api/webhooks/ci:deploy
        // That endpoint is unauthenticated and reachable from the
        // network by default.
        {
            name: "deploy",
            category: "ci",
            cwd: "/srv/app",
            stages: [
                { name: "build",  script: "./scripts/build.sh", timeout: "10m" },
                { name: "release", script: "./scripts/release.sh" }
            ]
        }
    ]
};
