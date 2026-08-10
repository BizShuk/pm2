package cmd

import (
	"strings"
	"testing"
)

// TestLaunchdPlistRunsInForeground pins the property that produced two
// daemons against one ~/.pm2: a plist invoking bare `daemon start`
// registers a job whose direct child detaches and exits, leaving the
// real daemon unsupervised and invisible to the next start attempt.
func TestLaunchdPlistRunsInForeground(t *testing.T) {
	plist := launchdPlist("com.shuk.pm2", "/usr/local/bin/pm2", "/usr/bin", "/home/u/.pm2")

	if !strings.Contains(plist, "<string>--foreground</string>") {
		t.Fatalf("plist does not pass --foreground:\n%s", plist)
	}
}

// TestLaunchdPlistKeepsAliveOnlyOnFailure guards the interaction between
// supervision and `pm2 daemon stop`: both stop verbs end in os.Exit(0),
// so an unconditional KeepAlive would respawn the daemon the user just
// stopped and quietly defeat the stop marker.
func TestLaunchdPlistKeepsAliveOnlyOnFailure(t *testing.T) {
	plist := launchdPlist("com.shuk.pm2", "/usr/local/bin/pm2", "/usr/bin", "/home/u/.pm2")

	if !strings.Contains(plist, "<key>KeepAlive</key>") {
		t.Fatal("plist does not request KeepAlive")
	}
	if !strings.Contains(plist, "<key>SuccessfulExit</key><false/>") {
		t.Fatalf("KeepAlive is not restricted to unsuccessful exits:\n%s", plist)
	}
	if strings.Contains(plist, "<key>KeepAlive</key><true/>") {
		t.Fatal("KeepAlive is unconditional; a clean `pm2 daemon stop` would be undone")
	}
}

// TestSystemdUnitMatchesLaunchdContract keeps the two supervisors
// saying the same thing. They drifted once already — the launchd job
// and the systemd unit are the same bug written twice.
func TestSystemdUnitMatchesLaunchdContract(t *testing.T) {
	unit := systemdUnit("/usr/local/bin/pm2", "/usr/bin")

	for _, want := range []string{
		"ExecStart=/usr/local/bin/pm2 daemon start --foreground",
		"Restart=on-failure",
		"Type=simple",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q:\n%s", want, unit)
		}
	}
	if strings.Contains(unit, "Restart=always") {
		t.Error("Restart=always would undo a deliberate `pm2 daemon stop`")
	}
}

// TestRestartThrottleIsSet keeps a failing daemon from being respawned
// as fast as the OS can fork — the shape of the crash loop that wrote
// 135 MB of repeated usage text to daemon-err.log.
func TestRestartThrottleIsSet(t *testing.T) {
	if restartThrottleSeconds <= 0 {
		t.Fatalf("restartThrottleSeconds = %d, want a positive floor", restartThrottleSeconds)
	}

	plist := launchdPlist("com.shuk.pm2", "/usr/local/bin/pm2", "/usr/bin", "/home/u/.pm2")
	if !strings.Contains(plist, "<key>ThrottleInterval</key>") {
		t.Error("plist does not throttle restarts")
	}
	if unit := systemdUnit("/usr/local/bin/pm2", "/usr/bin"); !strings.Contains(unit, "RestartSec=") {
		t.Error("unit does not throttle restarts")
	}
}
