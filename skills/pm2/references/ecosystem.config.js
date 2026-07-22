// ecosystem.config.js — Comprehensive reference for pm2
//
// Place this file at the repo root. pm2 resolves relative `script`
// paths against the directory of this config file, not the CWD.
//
// Supported formats: .js (module.exports = {...}) and .json.
// The .js format is preferred because it supports comments, __dirname,
// and expressions.
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
//   autorestart   bool     Restart on crash (default: true, set false for one-shot)
//   max_restarts  int      Crash-restart ceiling (default: 15)
//   cwd           string   Working directory for the spawned process
//   out_file      string   Custom stdout log path
//   error_file    string   Custom stderr log path
//   config_dir    string   Override ~/.config/<name>/ config root
//
// ─── Conventions ───────────────────────────────────────────────────
//
//   - One ecosystem.config.js per repo root.
//   - Use `namespace` to group by concern: "Service", "Local", "Agent", "planner".
//   - For one-shot cron tasks: set `cron` (not `cron_restart`).
//   - For long-running daemons that restart on a schedule: set `cron_restart`.
//   - Log paths default to ~/.config/<normalised-name>/logs/.
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
        // Pattern 4: AI agent planner with cron + __dirname
        // ──────────────────────────────────────────────────────────
        // __dirname is available in .js configs (goja runtime).
        // `autorestart: false` prevents crash-restarts between fires.
        {
            name: "agy-system-planner",
            script: "agy",
            args: [
                "--add-dir",
                __dirname,
                "-p",
                "run /system-planner for current workspace"
            ],
            namespace: "planner",
            instances: 1,
            cron: "10 0-9 * * *",
            autorestart: false,
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
    ]
};
