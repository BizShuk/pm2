package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/runhistory"
)

// readTaskJournal returns every task record the manager has written today.
func readTaskJournal(t *testing.T, home string) []runhistory.TaskRecord {
	t.Helper()
	path := filepath.Join(runhistory.TasksDir(home), time.Now().Format("2006-01-02")+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read journal: %v", err)
	}
	var out []runhistory.TaskRecord
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var rec runhistory.TaskRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("bad journal line %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

func waitForRecords(t *testing.T, home string, want int) []runhistory.TaskRecord {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var recs []runhistory.TaskRecord
	for time.Now().Before(deadline) {
		recs = readTaskJournal(t, home)
		if len(recs) >= want {
			return recs
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("want at least %d journal records, got %d after 5s", want, len(recs))
	return nil
}

// TestRunRecordCarriesExitCode is the whole point of the ExitInfo work:
// before it, a task that failed and a task that succeeded left
// indistinguishable traces.
func TestRunRecordCarriesExitCode(t *testing.T) {
	home := t.TempDir()
	pm := newTestPM(t, home)
	defer pm.KillAll()

	if _, err := pm.StartApp(&model.AppStartReq{AppConfig: process.AppConfig{
		Name: "failing", Script: "exit 3", MaxRestarts: 0,
	}}); err != nil {
		t.Fatalf("StartApp: %v", err)
	}

	recs := waitForRecords(t, home, 1)
	rec := recs[0]

	if rec.Event != runhistory.EventRun {
		t.Errorf("event: want %q, got %q", runhistory.EventRun, rec.Event)
	}
	if rec.Name != "failing" {
		t.Errorf("name: got %q", rec.Name)
	}
	if rec.ExitCode == nil || *rec.ExitCode != 3 {
		t.Errorf("exit code: want 3, got %v", rec.ExitCode)
	}
	if rec.Status != runhistory.StatusFailed {
		t.Errorf("status: want %q, got %q", runhistory.StatusFailed, rec.Status)
	}
	if rec.Trigger != runhistory.TriggerManual {
		t.Errorf("trigger: want %q, got %q", runhistory.TriggerManual, rec.Trigger)
	}
	if rec.PID == 0 {
		t.Error("record should carry the PID that ran, not the zero left behind")
	}
}

func TestRunRecordSuccess(t *testing.T) {
	home := t.TempDir()
	pm := newTestPM(t, home)
	defer pm.KillAll()

	if _, err := pm.StartApp(&model.AppStartReq{AppConfig: process.AppConfig{
		Name: "clean", Script: "true", MaxRestarts: 0,
	}}); err != nil {
		t.Fatalf("StartApp: %v", err)
	}

	rec := waitForRecords(t, home, 1)[0]
	if rec.ExitCode == nil || *rec.ExitCode != 0 {
		t.Errorf("exit code: want 0, got %v", rec.ExitCode)
	}
	if rec.Status != runhistory.StatusSuccess {
		t.Errorf("status: want %q, got %q", runhistory.StatusSuccess, rec.Status)
	}
}

// TestCronOkMeansExitedZero was impossible to write before this change:
// LastCronStatus was set to "ok" the moment the child spawned, so a cron
// task that failed every single night reported healthy forever.
func TestCronOkMeansExitedZero(t *testing.T) {
	home := t.TempDir()
	pm := newTestPM(t, home)
	defer pm.KillAll()

	req := &model.AppStartReq{AppConfig: process.AppConfig{
		Name: "nightly", Script: "exit 3", Cron: "@every 1h", MaxRestarts: 0,
	}}
	if _, err := pm.StartApp(req); err != nil {
		t.Fatalf("StartApp: %v", err)
	}

	pm.triggerCron(process.DefaultNamespace, "nightly", req)

	key := cronKey(process.DefaultNamespace, "nightly")
	deadline := time.Now().Add(5 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		if info, ok := pm.reg.SnapshotOne(key); ok {
			status = info.LastCronStatus
			if status == "ok" || status == "failed" {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	if status != "failed" {
		t.Errorf("a cron task exiting 3 must end at %q, got %q", "failed", status)
	}

	recs := waitForRecords(t, home, 1)
	last := recs[len(recs)-1]
	if last.Trigger != runhistory.TriggerCron {
		t.Errorf("trigger: want %q, got %q", runhistory.TriggerCron, last.Trigger)
	}
	if last.ExitCode == nil || *last.ExitCode != 3 {
		t.Errorf("journal exit code: want 3, got %v", last.ExitCode)
	}
}

// TestCronFireSkipIsJournaled: a dropped fire produces no run, so
// nothing else would record it — and "fired and skipped" must stay
// distinguishable from "never fired".
func TestCronFireSkipIsJournaled(t *testing.T) {
	home := t.TempDir()
	pm := newTestPM(t, home)
	defer pm.KillAll()

	req := &model.AppStartReq{AppConfig: process.AppConfig{
		Name: "slow", Script: "sleep 30", Cron: "@every 1h", MaxRestarts: 0,
	}}
	if _, err := pm.StartApp(req); err != nil {
		t.Fatalf("StartApp: %v", err)
	}

	pm.triggerCron(process.DefaultNamespace, "slow", req) // launches
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if info, ok := pm.reg.SnapshotOne(cronKey(process.DefaultNamespace, "slow")); ok && info.PID != 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	pm.triggerCron(process.DefaultNamespace, "slow", req) // must be dropped

	var skip *runhistory.TaskRecord
	for _, rec := range waitForRecords(t, home, 1) {
		if rec.Event == runhistory.EventCronSkip {
			r := rec
			skip = &r
		}
	}
	if skip == nil {
		t.Fatal("a dropped cron fire must leave a record")
	}
	if skip.Status != runhistory.StatusSkipped {
		t.Errorf("status: want %q, got %q", runhistory.StatusSkipped, skip.Status)
	}
	if skip.ExitCode != nil {
		t.Errorf("a fire that produced no run has no exit code, got %v", *skip.ExitCode)
	}
}

// TestHistoryFailureDoesNotBlockLaunch: the journal is an observability
// artifact. A task must start even when its history cannot be written.
func TestHistoryFailureDoesNotBlockLaunch(t *testing.T) {
	home := t.TempDir()

	// Occupy the journal directory's path with a file, so MkdirAll fails.
	if err := os.MkdirAll(filepath.Join(home, "tasks"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(runhistory.TasksDir(home), []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("block journal dir: %v", err)
	}

	pm := newTestPM(t, home)
	defer pm.KillAll()

	infos, err := pm.StartApp(&model.AppStartReq{AppConfig: process.AppConfig{
		Name: "resilient", Script: "sleep 5", MaxRestarts: 0,
	}})
	if err != nil {
		t.Fatalf("a broken journal must not fail a launch: %v", err)
	}
	if len(infos) != 1 || infos[0].PID == 0 {
		t.Fatalf("want one running process, got %#v", infos)
	}
}

// TestManualRestartCarriesRestartTrigger pins that the trigger travels
// from the launch path to the journal rather than defaulting.
func TestRunRecordCarriesTrigger(t *testing.T) {
	home := t.TempDir()
	pm := newTestPM(t, home)
	defer pm.KillAll()

	if _, err := pm.StartApp(&model.AppStartReq{AppConfig: process.AppConfig{
		Name: "svc", Script: "sleep 30", MaxRestarts: 0,
	}}); err != nil {
		t.Fatalf("StartApp: %v", err)
	}
	if err := pm.RestartByName("svc"); err != nil {
		t.Fatalf("RestartByName: %v", err)
	}
	if err := pm.StopByName("svc"); err != nil {
		t.Fatalf("StopByName: %v", err)
	}

	recs := waitForRecords(t, home, 2)
	var triggers []string
	for _, rec := range recs {
		triggers = append(triggers, rec.Trigger)
	}
	if triggers[0] != runhistory.TriggerManual {
		t.Errorf("first run: want %q, got %q", runhistory.TriggerManual, triggers[0])
	}
	if triggers[1] != runhistory.TriggerRestart {
		t.Errorf("restarted run: want %q, got %q (triggers=%v)", runhistory.TriggerRestart, triggers[1], triggers)
	}
}

// waitForStatus blocks until a process reaches the wanted status. It
// reads through SnapshotOne, the sanctioned value-copy path, because a
// naked read of mp.Info races onProcessExit's own write.
func waitForStatus(t *testing.T, pm *ProcessManager, key string, want process.Status) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if info, ok := pm.reg.SnapshotOne(key); ok && info.Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not reach status %q within 5s", key, want)
}

// TestWaitForExitsDrainsTheRunJournal pins the ordering that made every
// caller downstream of a finished process racy: executor.Watch closes
// `done` before it runs onProcessExit, and the status write inside that
// callback happens before the journal append. So "the registry says
// stopped" is precisely the window in which the record is not yet on
// disk — the window a test's temp-directory cleanup used to delete the
// directory the append then recreated.
func TestWaitForExitsDrainsTheRunJournal(t *testing.T) {
	home := t.TempDir()
	pm := newTestPM(t, home)

	if _, err := pm.StartApp(&model.AppStartReq{AppConfig: process.AppConfig{
		Name: "quick", Script: "true", MaxRestarts: 0,
	}}); err != nil {
		t.Fatalf("StartApp: %v", err)
	}
	waitForStatus(t, pm, "default:quick", process.StatusStopped)

	if !pm.WaitForExits(exitDrainTimeout) {
		t.Fatal("exit bookkeeping did not drain within the timeout")
	}
	if recs := readTaskJournal(t, home); len(recs) != 1 {
		t.Fatalf("journal holds %d records after the drain, want 1", len(recs))
	}
}

// TestKillAllJournalsTheRunsItEnded is the product half of the same
// fix: the dispatcher calls os.Exit ~150 ms after KillAll returns, so a
// KillAll that returned while the append was in flight would drop the
// record of a run the daemon itself had just ended.
func TestKillAllJournalsTheRunsItEnded(t *testing.T) {
	home := t.TempDir()
	pm := newTestPM(t, home)

	if _, err := pm.StartApp(&model.AppStartReq{AppConfig: process.AppConfig{
		Name: "longrunner", Script: "sleep", Args: []string{"30"}, MaxRestarts: 0,
	}}); err != nil {
		t.Fatalf("StartApp: %v", err)
	}

	pm.KillAll()

	// No polling: the point is that the record is already there when
	// KillAll returns.
	recs := readTaskJournal(t, home)
	if len(recs) != 1 {
		t.Fatalf("journal holds %d records right after KillAll, want 1", len(recs))
	}
	if recs[0].Name != "longrunner" {
		t.Errorf("journalled %q, want longrunner", recs[0].Name)
	}
}
