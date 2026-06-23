module.exports = {
    apps: [
        // port_listenor (default)
        {
            name: "port_listenor",
            script: "port_listenor",
            args: ["monitor"],
            namespace: "default",
            instances: 1,
        }
    ],
};
