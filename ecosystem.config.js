module.exports = {
    apps: [
        // agy-pm2-system (planner)
        // {
        //     name: "agy-pm2-system",
        //     script: "/Users/shuk/.local/bin/agy",
        //     args: ["--add-dir", "/Users/shuk/projects/tmp/pm2", "-p", "'run /system-planner for current workspace'"],
        //     namespace: "planner",
        //     instances: 1,
        //     cron: "20 0-9 * * *",
        // },
        // cron_test (default)
        {
            name: "cron_test",
            script: "/bin/echo",
            args: ["hello", "world"],
            namespace: "default",
            instances: 1,
            cron: "* * * * *",
        },
        // Claudew Planner test (planner)
        // {
        //     name: "Claudew Planner test",
        //     script: "claudew",
        //     args: ["-p", "'test test'"],
        //     namespace: "planner",
        //     instances: 1,
        // }
    ],
};
