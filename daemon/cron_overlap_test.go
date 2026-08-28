package daemon

import (
	"testing"
	"time"

	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
)

// TestCronFireSkippedWhileRunning is the regression test for the overlap
// guard in triggerCron.
//
// Before the guard, every fire began with stopProcess: a cron task whose run
// outlives its own interval was SIGTERMed mid-work on each tick and restarted
// from scratch, so it could never complete once. The schedule now yields to
// the run in flight — the fire is dropped and recorded as "skipped", and the
// original child keeps its PID.
func TestCronFireSkippedWhileRunning(t *testing.T) {
	testDir := testDir(t)
	pm := newTestPM(t, testDir)

	const key = "default:cron-overlap-app"

	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
			Namespace: "default",
			Name:      "cron-overlap-app",
			// Runs far longer than the interval, so every fire after the
			// first one lands on top of a live child.
			Script:    "/bin/sleep",
			Args:      []string{"60"},
			Instances: 1,
			Cron:      "@every 1s",
		},
	}
	if _, err := pm.StartApp(req); err != nil {
		t.Fatalf("startApp failed: %v", err)
	}
	defer pm.StopByName("cron-overlap-app")

	// A cron task registers idle; the first fire is what spawns the child.
	firstPID := 0
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if info, ok := pm.reg.SnapshotOne(key); ok && info.PID != 0 {
			firstPID = info.PID
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if firstPID == 0 {
		t.Fatalf("first cron fire never launched a process")
	}

	// The next fire must be dropped rather than replacing the running child.
	var last process.ProcessInfo
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		info, ok := pm.reg.SnapshotOne(key)
		if ok {
			last = info
			if info.LastCronStatus == CronStatusSkipped {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	if last.LastCronStatus != CronStatusSkipped {
		t.Fatalf("LastCronStatus = %q, want %q — the overlapping fire was not skipped",
			last.LastCronStatus, CronStatusSkipped)
	}
	if last.PID != firstPID {
		t.Errorf("PID = %d, want %d — the skipped fire replaced the running process",
			last.PID, firstPID)
	}
	if last.LastCronAt.IsZero() {
		t.Errorf("LastCronAt is zero — a skipped fire is still a fire and must be timestamped")
	}
}

// TestCronFireRunsWhenIdle pins the other half of the guard: an idle cron
// task (PID 0 between fires) must still launch. A guard that keyed off
// "the entry exists" rather than "a run is in flight" would skip forever.
func TestCronFireRunsWhenIdle(t *testing.T) {
	testDir := testDir(t)
	pm := newTestPM(t, testDir)

	const key = "default:cron-idle-app"

	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
			Namespace: "default",
			Name:      "cron-idle-app",
			// Exits immediately, so each fire finds the task idle again.
			Script:    "/usr/bin/true",
			Instances: 1,
			Cron:      "@every 1s",
		},
	}
	if _, err := pm.StartApp(req); err != nil {
		t.Fatalf("startApp failed: %v", err)
	}
	defer pm.StopByName("cron-idle-app")

	var last process.ProcessInfo
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		info, ok := pm.reg.SnapshotOne(key)
		if ok {
			last = info
			if info.LastCronStatus == "ok" {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	if last.LastCronStatus != "ok" {
		t.Fatalf("LastCronStatus = %q, want \"ok\" — an idle cron task did not fire",
			last.LastCronStatus)
	}
}
