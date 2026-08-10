package runtime

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// noHomeChildEnv marks the re-executed child so it does not recurse.
const noHomeChildEnv = "PM2_TEST_NO_HOME_CHILD"

// TestPackageInitSurvivesMissingHome re-runs this test binary with no
// $HOME at all.
//
// The regression it pins: resolving ~/.pm2 in a package init() killed
// the whole process before main ran on any host with no HOME. launchd
// gives a system-domain LaunchDaemon exactly that environment, so
// `pm2 gpu agent` — which never touches ~/.pm2 — exited 1 on every
// spawn, and the only evidence was a home-directory message in a log
// nobody would think to connect to the GPU agent.
//
// Reaching the child's test body at all is the assertion.
func TestPackageInitSurvivesMissingHome(t *testing.T) {
	if os.Getenv(noHomeChildEnv) == "1" {
		return
	}

	child := exec.Command(os.Args[0], "-test.run=TestPackageInitSurvivesMissingHome")
	child.Env = append(withoutHome(os.Environ()), noHomeChildEnv+"=1")

	output, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("child exited with %v; a package init still needs $HOME:\n%s", err, output)
	}
	if strings.Contains(string(output), "cannot determine home dir") {
		t.Errorf("child resolved the home directory it should not have needed:\n%s", output)
	}
}

// TestPM2HomeStillResolvesForCommandsThatNeedIt guards the other half:
// deferring the lookup must not stop it from happening.
func TestPM2HomeStillResolvesForCommandsThatNeedIt(t *testing.T) {
	home := PM2Home()
	if home == "" || !strings.HasSuffix(home, ".pm2") {
		t.Fatalf("PM2Home = %q, want a ~/.pm2 path", home)
	}
	if got := SocketPath(); !strings.HasPrefix(got, home) {
		t.Errorf("SocketPath = %q, want it under %q", got, home)
	}
	if got := DaemonStopMarkerPath(); !strings.HasPrefix(got, home) {
		t.Errorf("DaemonStopMarkerPath = %q, want it under %q", got, home)
	}
}

func withoutHome(environ []string) []string {
	kept := environ[:0:0]
	for _, entry := range environ {
		if strings.HasPrefix(entry, "HOME=") {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}
