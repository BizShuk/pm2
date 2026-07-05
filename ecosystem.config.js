module.exports = {
    apps: [
        // agy-pm2-system (planner)
        {
            name: "agy-pm2-system",
            script: "/Users/shuk/.local/bin/agy",
            args: ["--add-dir", "/Users/shuk/projects/tmp/pm2", "-p", "'run /system-planner for current workspace'"],
            namespace: "planner",
            cwd: "/Users/shuk/projects/tmp/pm2",
            instances: 1,
            cron: "20 0-9 * * *",
        },
        // cron_test (default)
        {
            name: "cron_test",
            script: "echo",
            args: ["hello", "world"],
            namespace: "default",
            instances: 1,
            cron: "* * * * *",
        }
    ],
};
