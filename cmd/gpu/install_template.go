package gpu

import "fmt"

// gpuAgentLabel identifies the LaunchDaemon job.
const gpuAgentLabel = "com.shuk.pm2.gpu"

// gpuRestartThrottleSeconds is the minimum gap between two supervised
// restarts, for the same reason the daemon's job has one: a job that
// fails on startup fails instantly, and without a floor launchd
// respawns it as fast as the OS can fork.
const gpuRestartThrottleSeconds = 10

// gpuAgentErrLog is where launchd sends whatever the agent writes to
// stderr — the only place a `powermetrics` failure is visible, since
// the agent has no terminal.
const gpuAgentErrLog = "/var/log/pm2-gpu.err.log"

// launchDaemonPlist renders the system-domain job definition.
//
// Three properties carry the contract:
//
//   - A LaunchDaemon, not a LaunchAgent. `pm2 startup` installs into
//     ~/Library/LaunchAgents because the pm2 daemon must run as the
//     user who owns ~/.config/pm2. This job is the exact opposite: it exists
//     only because it needs root, so it belongs in the system domain.
//   - `gpu agent` runs in the foreground by construction, so launchd's
//     direct child is the real worker and the job's reported state is
//     the truth.
//   - The interval is baked in only when the operator names one. With
//     no `--interval` the argv omits the flag and the agent uses its own
//     default, so the sampling period tracks the binary instead of a
//     plist written months ago.
//   - `KeepAlive` is unconditional here, unlike the daemon's
//     `SuccessfulExit = false`. The daemon has `pm2 daemon stop`, whose
//     clean exit an unconditional restart would undo. The agent has no
//     such verb: the only way to stop it is `launchctl bootout`, which
//     removes the job rather than letting it exit, so there is no clean
//     exit to respect and any other exit means powermetrics died.
func launchDaemonPlist(exe, outPath string, intervalSeconds int) string {
	intervalArgs := ""
	if intervalSeconds > 0 {
		intervalArgs = fmt.Sprintf("\n\t<string>--interval</string>\n\t<string>%ds</string>", intervalSeconds)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key>
	<array>
	<string>%s</string>
	<string>gpu</string>
	<string>agent</string>
	<string>--out</string>
	<string>%s</string>%s
	</array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>ThrottleInterval</key><integer>%d</integer>
	<key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, gpuAgentLabel, exe, outPath, intervalArgs, gpuRestartThrottleSeconds, gpuAgentErrLog)
}
