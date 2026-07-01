module.exports = {
    apps: [
        // agy-pm2 (planner)
        {
            name: "agy-pm2-system",
            script: "agy",
            args: [
                "--add-dir",
                "/Users/shuk/projects/tmp/pm2",
                "-p",
                "'run /system-planner for current workspace'"
            ],
            namespace: "planner",
            cwd: "/Users/shuk/projects/tmp/pm2",
            instances: 1,
            cron: "20 0-9 * * *"
        }
    ]
};
