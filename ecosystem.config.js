module.exports = {
    apps: [
        // claudew-pm2 (planner)
        {
            name: "claudew-pm2",
            script: "claudew",
            args: ["-p", "[plan only] run /system-planner and output to ./plans/", "test"],
            namespace: "planner",
            instances: 1,
        }
    ],
};
