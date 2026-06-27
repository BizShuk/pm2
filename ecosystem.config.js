module.exports = {
    apps: [
        // agy-pm2 (planner)
        {
            name: "agy-pm2",
            script: "/Users/shuk/.local/bin/agy",
            args: ["--add-dir", "/Users/shuk/projects/tmp/pm2", "-p", "'run /system-planner for current workspace, and output under <workspace>/plans/'"],
            namespace: "planner",
            cwd: "/Users/shuk/projects/tmp/pm2",
            instances: 1,
        },
        // claude-pm2 (planner)
        {
            name: "claude-pm2",
            script: "claude",
            args: ["--add-dir", "/Users/shuk/projects/tmp/pm2", "-p", "'run /business-planner for current workspace, and output under <workspace>/plans/'"],
            namespace: "planner",
            cwd: "/Users/shuk/projects/tmp/pm2",
            instances: 1,
        }
    ],
};
