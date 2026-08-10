package gpu

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bizshuk/pm2/sysmon"
)

func TestFormatStatusShowsTheCurrentReading(t *testing.T) {
	out := formatStatus("/var/run/pm2-gpu.json", &sysmon.GPU{
		Source:             "powermetrics",
		UtilizationPercent: 42.5,
		FrequencyMHz:       1398,
		PowerMilliwatts:    8231,
		SampledAt:          time.Now(),
		IntervalSeconds:    2,
	}, nil)

	for _, want := range []string{"publishing", "42.5%", "1398 MHz", "8231 mW", "powermetrics"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output missing %q:\n%s", want, out)
		}
	}
}

// The three failure states an operator actually hits must be told apart:
// nothing installed, something wedged, and something unreadable are
// different problems with different fixes.
func TestFormatStatusDistinguishesAbsentFromStale(t *testing.T) {
	absent := formatStatus("/var/run/pm2-gpu.json", nil,
		fmt.Errorf("open /var/run/pm2-gpu.json: %w", os.ErrNotExist))
	if !strings.Contains(absent, "no agent") || !strings.Contains(absent, "pm2 gpu install") {
		t.Errorf("absent status should name the install fix:\n%s", absent)
	}

	stale := formatStatus("/var/run/pm2-gpu.json", nil,
		fmt.Errorf("%w: 40s old", sysmon.ErrGPUStale))
	if !strings.Contains(stale, "stale") || !strings.Contains(stale, gpuAgentLabel) {
		t.Errorf("stale status should name the job to restart:\n%s", stale)
	}
	if strings.Contains(stale, "no agent") {
		t.Errorf("a wedged agent must not be reported as an absent one:\n%s", stale)
	}

	broken := formatStatus("/var/run/pm2-gpu.json", nil, fmt.Errorf("invalid character 'x'"))
	if !strings.Contains(broken, "unreadable") {
		t.Errorf("parse failure should read as unreadable:\n%s", broken)
	}
}

// The LaunchDaemon carries the same supervision contract the pm2 daemon
// job does, and pm2 has already had two service definitions drift apart
// once. These are the properties that make the difference between a
// supervised agent and an invisible one.
func TestLaunchDaemonPlistCarriesTheSupervisionContract(t *testing.T) {
	plist := launchDaemonPlist("/usr/local/bin/pm2", "/var/run/pm2-gpu.json", 2)

	// The job must run the agent verb directly: `gpu agent` never
	// detaches, so launchd's direct child is the real worker.
	for _, want := range []string{
		"<string>gpu</string>",
		"<string>agent</string>",
		"<string>/var/run/pm2-gpu.json</string>",
		"<string>2s</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %s:\n%s", want, plist)
		}
	}

	// Unconditional KeepAlive, unlike the daemon's job: the agent has no
	// `stop` verb whose clean exit a restart would undo.
	if !strings.Contains(plist, "<key>KeepAlive</key><true/>") {
		t.Errorf("plist should keep the agent alive unconditionally:\n%s", plist)
	}
	if strings.Contains(plist, "SuccessfulExit") {
		t.Errorf("plist should not copy the daemon's conditional KeepAlive:\n%s", plist)
	}

	// A throttle, because an agent that fails on startup fails instantly.
	if !strings.Contains(plist, fmt.Sprintf("<key>ThrottleInterval</key><integer>%d</integer>", gpuRestartThrottleSeconds)) {
		t.Errorf("plist missing the restart throttle:\n%s", plist)
	}

	// Somewhere to look when powermetrics refuses to start.
	if !strings.Contains(plist, gpuAgentErrLog) {
		t.Errorf("plist missing the stderr path:\n%s", plist)
	}
}
