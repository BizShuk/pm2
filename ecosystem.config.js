module.exports = {
    apps: [
        // agy-pm2 (planner)
        {
            name: "agy-pm2",
            script: "agy",
            args: ["--add-dir", "/Users/shuk/projects/tmp/pm2", "-p", "'run /system-planner for current workspace, and output under <workspace>/plans/'"],
            namespace: "planner",
            cwd: "/Users/shuk/projects/tmp/pm2",
            instances: 1,
            cron: "0 0-9 * * *",
        }
    ],
};
