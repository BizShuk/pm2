package daemon

import (
	"testing"
	"time"

	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
)

// TestPauseDuringCronFireLeavesNoSchedule is the regression test for the bug
// "a paused cron task still fires on its schedule".
//
// Root cause: executor.Start (the fork/exec) runs before launchProcess takes
// the registry lock. A cron fire that was already in-flight when PauseByName
// ran would, under the lock, overwrite the just-paused entry and re-register
// the schedule using the fire's original req (Paused=false) — silently
// undoing the pause. The fix gates that relaunch on the live paused state
// under the same lock PauseByName mutates.
//
// The existing TestPauseResumeCronTask only pauses a freshly-started idle task
// (before any fire), so it never exercised this race. Here the cron fires
// ~10x/sec while we repeatedly pause during active firing; after each pause
// the invariant "a paused process has zero scheduler entries" must hold.
func TestPauseDuringCronFireLeavesNoSchedule(t *testing.T) {
	testDir := testDir(t)
	s := NewServer(testDir)

	req := &model.AppStartReq{
		AppConfig: process.AppConfig{
			Namespace: "default",
			Name:      "racecron",
			Script:    "/bin/echo",
			Args:      []string{"tick"},
			Cron:      "@every 100ms", // fast enough to overlap pause with a fire
		},
	}
	if _, err := s.StartApp(req); err != nil {
		t.Fatalf("startApp: %v", err)
	}

	for i := range 40 {
		time.Sleep(70 * time.Millisecond) // land near a fire boundary
		if err := s.PauseByName("default:racecron"); err != nil {
			t.Fatalf("pause iter %d: %v", i, err)
		}
		// Let any in-flight fire finish its (guarded) relaunch attempt.
		time.Sleep(20 * time.Millisecond)

		mp, _ := s.reg.Get("default:racecron")
		if mp.Info.Status != process.StatusPaused {
			t.Fatalf("iter %d: status=%s, want paused", i, mp.Info.Status)
		}
		if got := s.scheduler.EntryCount(); got != 0 {
			t.Fatalf("iter %d: paused task has %d scheduler entries, want 0 — it would keep firing", i, got)
		}

		if err := s.ResumeByName("default:racecron"); err != nil {
			t.Fatalf("resume iter %d: %v", i, err)
		}
	}

	_ = s.StopByName("default:racecron")
}
