module.exports = {
    apps: [
        // agy-pm2 (planner)
        {
            name: "agy-pm2",
            script: "agy",
            args: ["-p", "[plan only] run /system-planner and output to ${cwd}/plans/", ""],
            namespace: "planner",
            cwd: "/Users/bytedance/projects/pm2",
            instances: 1,
            cron: "* * * * *"
        },
    ],
};
