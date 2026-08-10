package cmd

import "fmt"

// restartThrottleSeconds is the minimum gap between two supervised
// restarts. A daemon that fails on startup fails instantly — without a
// floor, the supervisor respawns it as fast as the OS can fork, and the
// only visible result is an append-only error log growing by megabytes
// a minute (see the SilenceUsage note on the root command).
const restartThrottleSeconds = 10

// launchdPlist renders the LaunchAgent definition for the daemon.
//
// Two properties carry the supervision contract:
//
//   - `daemon start --foreground` — bare `daemon start` re-execs and
//     detaches, so launchd's direct child exits 0 immediately, the job
//     is recorded as `state = not running`, and the real daemon
//     reparents to PID 1 where no supervisor can see it.
//   - `KeepAlive` restricted to `SuccessfulExit = false` — restart the
//     daemon when it dies, but NOT when it exits cleanly. Both
//     `pm2 daemon stop` and `pm2 daemon kill` end in os.Exit(0), so a
//     plain `KeepAlive = true` would respawn the daemon the user just
//     deliberately stopped, silently defeating the stop marker.
func launchdPlist(label, exe, path, home string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key>
	<array>
	<string>%s</string>
	<string>daemon</string>
	<string>start</string>
	<string>--foreground</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
	<key>PATH</key>
	<string>%s</string>
	</dict>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key>
	<dict>
	<key>SuccessfulExit</key><false/>
	</dict>
	<key>ThrottleInterval</key><integer>%d</integer>
	<key>StandardOutPath</key><string>%s/daemon.log</string>
	<key>StandardErrorPath</key><string>%s/daemon-err.log</string>
</dict>
</plist>
`, label, exe, path, restartThrottleSeconds, home, home)
}

// systemdUnit renders the user unit for the daemon.
//
// `Restart=on-failure` is the systemd spelling of launchd's
// `SuccessfulExit = false`: restart on a non-zero exit or a fatal
// signal, stay down after the clean exit that `pm2 daemon stop` and
// `pm2 daemon kill` produce. `Restart=always` would undo a deliberate
// stop. `Type=simple` is correct only because ExecStart runs in the
// foreground — the same reason the launchd job needs `--foreground`.
func systemdUnit(exe, path string) string {
	return fmt.Sprintf(`[Unit]
Description=PM2 Daemon
After=network.target

[Service]
Type=simple
ExecStart=%s daemon start --foreground
Restart=on-failure
RestartSec=%d
Environment="PATH=%s"

[Install]
WantedBy=default.target
`, exe, restartThrottleSeconds, path)
}
