package dashboard

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/pm2/sysmon"
)

func observationFixture() sysmon.Observation {
	return sysmon.Observation{
		Snapshot: sysmon.Snapshot{
			Time: time.Now(),
			Host: sysmon.Host{Hostname: "test-host", Cores: 10, UptimeSeconds: 90000},
			System: sysmon.System{
				CPU:     sysmon.CPU{Cores: 10, UsedPercent: 24, UserPercent: 9, SysPercent: 15, IdlePercent: 76},
				Memory:  sysmon.Memory{TotalBytes: 16 << 30, UsedBytes: 15 << 30, UsedPercent: 93.7, AvailableBytes: 3 << 30},
				Load:    sysmon.Load{One: 3.1, Five: 5.9, Fifteen: 6.3},
				Network: sysmon.Network{Interface: "en0", RxBytesPerSecond: 1 << 20, TxBytesPerSecond: 1 << 18},
				DiskIO:  sysmon.DiskIO{BytesPerSecond: 8 << 20, TransfersPerSecond: 947},
				Disks:   []sysmon.Disk{{Mount: "/", TotalBytes: 228 << 30, UsedBytes: 11 << 30, UsedPercent: 5.1}},
			},
			Processes: sysmon.ProcCounts{Total: 3, Running: 1},
			Tasks: []sysmon.Task{
				{
					ID: 1, Namespace: "Service", Name: "api", Status: "online", PID: 1978,
					CPUPercent: 2, MemoryBytes: 1 << 20, TreeCPUPercent: 20, TreeMemoryBytes: 4 << 20,
					Children: []sysmon.Proc{{PID: 2001, PPID: 1978, CPUPercent: 18, MemoryBytes: 3 << 20, Command: "/bin/worker"}},
					Ports:    []sysmon.Port{{PID: 2001, Protocol: "tcp", Address: "0.0.0.0", Port: 8080, State: "LISTEN"}},
				},
				{
					ID: 2, Namespace: "default", Name: "backup", Status: "stopped",
					CPUPercent: 0, MemoryBytes: 0, TreeCPUPercent: 0, TreeMemoryBytes: 0,
					Children: []sysmon.Proc{}, Ports: []sysmon.Port{},
				},
			},
		},
		Procs: []sysmon.Proc{
			{PID: 1978, PPID: 1, CPUPercent: 2, MemoryBytes: 1 << 20, State: "S", Command: "/bin/api"},
			{PID: 2001, PPID: 1978, CPUPercent: 18, MemoryBytes: 3 << 20, State: "R", Command: "/bin/worker"},
			{PID: 3000, PPID: 1, CPUPercent: 50, MemoryBytes: 9 << 20, State: "R", Command: "/bin/hog --loud"},
		},
		Ports: map[int][]sysmon.Port{
			2001: {{PID: 2001, Protocol: "tcp", Address: "0.0.0.0", Port: 8080, State: "LISTEN"}},
		},
	}
}

// loaded returns a model that has received one observation, the state
// every interaction test starts from.
func loaded(t *testing.T) Model {
	t.Helper()
	model, _ := New("/tmp/does-not-exist.sock").Update(observationMsg{observation: observationFixture()})
	dashboard, ok := model.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want dashboard.Model", model)
	}
	return dashboard
}

func press(t *testing.T, model Model, key string) Model {
	t.Helper()
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	updated, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want dashboard.Model", next)
	}
	return updated
}

func TestDefaultScopeIsManagedTasks(t *testing.T) {
	// pm2's own applications are what a pm2 user came for; the whole
	// machine is one keystroke away.
	model := loaded(t)
	if model.scope != ScopeTasks {
		t.Errorf("scope = %q, want %q", model.scope, ScopeTasks)
	}
	if model.rowCount() != 2 {
		t.Errorf("rowCount = %d, want the two managed tasks", model.rowCount())
	}
}

func TestScopeToggleSwitchesListAndResetsCursor(t *testing.T) {
	model := loaded(t)
	model.selected = 1

	model = press(t, model, "a")

	if model.scope != ScopeSystem {
		t.Fatalf("scope = %q, want %q", model.scope, ScopeSystem)
	}
	if model.rowCount() != 3 {
		t.Errorf("rowCount = %d, want every OS process", model.rowCount())
	}
	// Row 1 of a task list and row 1 of a 600-process table are unrelated;
	// carrying the index across would read as a random jump.
	if model.selected != 0 {
		t.Errorf("selected = %d, want the cursor reset to the top", model.selected)
	}

	if model = press(t, model, "a"); model.scope != ScopeTasks {
		t.Errorf("scope = %q, want the toggle to return to tasks", model.scope)
	}
}

func TestSortDefaultsToCPUAndCycles(t *testing.T) {
	model := loaded(t)

	if model.sortBy != SortByCPU {
		t.Errorf("sortBy = %q, want cpu", model.sortBy)
	}
	// The busiest task leads: that is the question an activity monitor
	// exists to answer.
	if model.observation.Snapshot.Tasks[0].Name != "api" {
		t.Errorf("first task = %q, want the highest tree CPU first",
			model.observation.Snapshot.Tasks[0].Name)
	}

	model = press(t, model, "s")
	if model.sortBy != SortByMemory {
		t.Fatalf("sortBy = %q, want memory after one press", model.sortBy)
	}
	model = press(t, model, "s")
	if model.sortBy != SortByName {
		t.Fatalf("sortBy = %q, want name after two presses", model.sortBy)
	}
	model = press(t, model, "s")
	if model.sortBy != SortByCPU {
		t.Errorf("sortBy = %q, want the cycle to return to cpu", model.sortBy)
	}
}

func TestSystemScopeRanksByCPU(t *testing.T) {
	model := press(t, loaded(t), "a")
	if model.ranked[0].PID != 3000 {
		t.Errorf("first process = %d, want the 50%% CPU process 3000", model.ranked[0].PID)
	}
}

func TestSortByNameOrdersAlphabetically(t *testing.T) {
	model := press(t, loaded(t), "s") // memory
	model = press(t, model, "s")      // name
	if model.observation.Snapshot.Tasks[0].Name != "api" {
		t.Errorf("first task = %q, want api before backup", model.observation.Snapshot.Tasks[0].Name)
	}
}

func TestNavigationStaysInBounds(t *testing.T) {
	model := loaded(t)

	for range 5 {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = next.(Model)
	}
	if model.selected != model.rowCount()-1 {
		t.Errorf("selected = %d after paging past the end, want %d", model.selected, model.rowCount()-1)
	}

	for range 5 {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
		model = next.(Model)
	}
	if model.selected != 0 {
		t.Errorf("selected = %d after paging past the start, want 0", model.selected)
	}
}

func TestSelectionClampsWhenTasksDisappear(t *testing.T) {
	// A deleted task shrinks the list under a cursor that was pointing
	// past the new end.
	model := loaded(t)
	model.selected = 1

	shrunk := observationFixture()
	shrunk.Snapshot.Tasks = shrunk.Snapshot.Tasks[:1]
	next, _ := model.Update(observationMsg{observation: shrunk})

	if got := next.(Model).selected; got != 0 {
		t.Errorf("selected = %d, want 0 after the list shrank", got)
	}
}

func TestViewShowsSelectedTaskTreeAndPorts(t *testing.T) {
	model := loaded(t)
	model.width, model.height = 140, 40

	frame := model.View()

	for _, want := range []string{"pm2 taskmanager", "test-host", "api", "SUB-PROCESSES (1)", "LISTENING PORTS (1)", "8080", "2001"} {
		if !strings.Contains(frame, want) {
			t.Errorf("frame is missing %q\n%s", want, frame)
		}
	}
}

func TestViewShowsOSProcessDetailInSystemScope(t *testing.T) {
	model := press(t, loaded(t), "a")
	model.width, model.height = 140, 40

	frame := model.View()

	if !strings.Contains(frame, "hog") {
		t.Errorf("frame is missing the selected process name\n%s", frame)
	}
	if !strings.Contains(frame, "PROCESSES (3)") {
		t.Errorf("frame is missing the OS process count\n%s", frame)
	}
}

func TestViewBeforeFirstSampleDoesNotPanic(t *testing.T) {
	// Init fires a collection that takes about a second on macOS; the
	// first frame renders before it lands.
	frame := New("/tmp/does-not-exist.sock").View()
	if !strings.Contains(frame, "sampling") {
		t.Errorf("first frame should say it is still sampling\n%s", frame)
	}
}

func TestUnreachableDaemonStillRendersTheMachine(t *testing.T) {
	model := loaded(t)
	model.width, model.height = 140, 40
	next, _ := model.Update(observationMsg{
		observation: observationFixture(),
		notice:      "daemon unreachable — showing system only",
	})

	frame := next.(Model).View()
	if !strings.Contains(frame, "daemon unreachable") {
		t.Errorf("frame is missing the daemon notice\n%s", frame)
	}
	if !strings.Contains(frame, "cpu") {
		t.Error("frame dropped the host panel when the daemon was unreachable")
	}
}

func TestKillAsksBeforeActing(t *testing.T) {
	// `d` is one keystroke away on a 600-row process table, and neither
	// stopping a task nor signalling a process can be undone.
	model := press(t, loaded(t), "d")

	if model.confirm == nil {
		t.Fatal("d should arm a confirmation, not act immediately")
	}
	if model.confirm.system {
		t.Error("task scope should target the daemon, not a raw signal")
	}
	if model.confirm.label != "api" || model.confirm.id != "1" {
		t.Errorf("target = %+v, want the selected task api (id 1)", *model.confirm)
	}

	model.width, model.height = 140, 40
	if frame := model.View(); !strings.Contains(frame, "stop task api") {
		t.Errorf("frame is missing the confirmation prompt\n%s", frame)
	}

	if model = press(t, model, "n"); model.confirm != nil {
		t.Error("n should cancel the confirmation")
	}
}

func TestConfirmationSwallowsNavigation(t *testing.T) {
	// Moving the cursor under a prompt that names one process would leave
	// the prompt describing a row the user has already left.
	model := press(t, loaded(t), "d")
	model = press(t, model, "j")

	if model.selected != 0 {
		t.Errorf("selected = %d, want the cursor pinned while confirming", model.selected)
	}
	if model.confirm == nil {
		t.Error("an unrelated key should leave the confirmation armed")
	}
}

func TestKillInSystemScopeTargetsThePID(t *testing.T) {
	model := press(t, loaded(t), "a") // system scope, cursor on pid 3000
	model = press(t, model, "d")

	if model.confirm == nil {
		t.Fatal("d should arm a confirmation in system scope")
	}
	if !model.confirm.system || model.confirm.pid != 3000 {
		t.Errorf("target = %+v, want a signal to pid 3000", *model.confirm)
	}
}

func TestKillRefusesUnrunnableSelections(t *testing.T) {
	// A stopped task and an idle cron task both sit at PID 0; "stop" on
	// them means nothing, and a `d` that does nothing at all reads as a
	// broken key.
	model := loaded(t)
	model.selected = 1 // backup, stopped
	model = press(t, model, "d")

	if model.confirm != nil {
		t.Error("a task with no PID should not arm a confirmation")
	}
	if !strings.Contains(model.action, "not running") {
		t.Errorf("action = %q, want an explanation of the refusal", model.action)
	}
}

func TestKillRefusesToSignalItself(t *testing.T) {
	model := press(t, loaded(t), "a")
	model.ranked[0].PID = os.Getpid()
	model = press(t, model, "d")

	if model.confirm != nil {
		t.Fatal("the dashboard must not offer to kill itself")
	}
	if !strings.Contains(model.action, "itself") {
		t.Errorf("action = %q, want the self-kill refusal", model.action)
	}
}

func TestKillResultShowsThenExpires(t *testing.T) {
	model := loaded(t)
	next, _ := model.Update(killResultMsg{notice: "stopped api"})
	model = next.(Model)
	model.width, model.height = 140, 40

	if frame := model.View(); !strings.Contains(frame, "stopped api") {
		t.Errorf("frame is missing the kill result\n%s", frame)
	}

	// The result must not outlive the list catching up with it.
	model.actionAt = time.Now().Add(-2 * actionTTL)
	next, _ = model.Update(observationMsg{observation: observationFixture()})
	if got := next.(Model).action; got != "" {
		t.Errorf("action = %q, want it cleared after %s", got, actionTTL)
	}
}

func TestKillResultDoesNotStartASecondCollectionLoop(t *testing.T) {
	// Update re-arms exactly one collection chain from observationMsg;
	// refreshing from a kill result would leave two running forever.
	if _, cmd := loaded(t).Update(killResultMsg{notice: "stopped api"}); cmd != nil {
		t.Error("killResultMsg should not issue a command")
	}
}

func TestNarrowTerminalDegradesGracefully(t *testing.T) {
	model := loaded(t)
	model.width, model.height = 40, 20
	if frame := model.View(); !strings.Contains(frame, "too narrow") {
		t.Errorf("frame = %q, want the narrow-terminal notice", frame)
	}
}

// advance feeds one more collection into a loaded model, the way the
// refresh tick does.
func advance(t *testing.T, model Model, observation sysmon.Observation) Model {
	t.Helper()
	next, _ := model.Update(observationMsg{observation: observation})
	updated, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want dashboard.Model", next)
	}
	return updated
}

// The list is re-ranked on every collection, so a cursor that is only a
// row number silently changes subject underneath a reader.
func TestSelectionFollowsProcessAcrossReRank(t *testing.T) {
	model := press(t, press(t, loaded(t), "a"), "j") // system scope, second row
	want := model.ranked[model.selected].PID

	demoted := observationFixture()
	for i := range demoted.Procs {
		if demoted.Procs[i].PID == want {
			demoted.Procs[i].CPUPercent = 0 // now the least busy process
		}
	}
	model = advance(t, model, demoted)

	if got := model.ranked[model.selected].PID; got != want {
		t.Errorf("selected pid = %d after re-rank, want %d", got, want)
	}
	if model.selected != len(model.ranked)-1 {
		t.Errorf("selected row = %d, want the last row %d", model.selected, len(model.ranked)-1)
	}
}

func TestSelectionFollowsTaskAcrossReRank(t *testing.T) {
	model := press(t, loaded(t), "j")
	want := model.observation.Snapshot.Tasks[model.selected].Name

	promoted := observationFixture()
	for i := range promoted.Snapshot.Tasks {
		if promoted.Snapshot.Tasks[i].Name == want {
			promoted.Snapshot.Tasks[i].TreeCPUPercent = 99 // now the busiest task
		}
	}
	model = advance(t, model, promoted)

	if got := model.observation.Snapshot.Tasks[model.selected].Name; got != want {
		t.Errorf("selected task = %q after re-rank, want %q", got, want)
	}
}

// Cycling the sort order re-ranks too, and the row the user was reading
// is exactly the one they want to keep looking at.
func TestSortCycleKeepsTheSelectedTask(t *testing.T) {
	model := press(t, loaded(t), "j")
	want := model.observation.Snapshot.Tasks[model.selected].Name

	model = press(t, model, "s")

	if got := model.observation.Snapshot.Tasks[model.selected].Name; got != want {
		t.Errorf("selected task = %q after sort cycle, want %q", got, want)
	}
}

// A subject that exits leaves the cursor where it is on screen rather
// than throwing it back to the top of the list.
func TestSelectionFallsBackWhenSubjectDisappears(t *testing.T) {
	model := press(t, press(t, loaded(t), "a"), "j")
	gone := model.ranked[model.selected].PID

	shrunk := observationFixture()
	remaining := shrunk.Procs[:0]
	for _, proc := range shrunk.Procs {
		if proc.PID != gone {
			remaining = append(remaining, proc)
		}
	}
	shrunk.Procs = remaining
	model = advance(t, model, shrunk)

	if model.selected != 1 {
		t.Errorf("selected row = %d after the subject exited, want 1", model.selected)
	}
}

func TestRefreshDelayHonoursIntervalAndFloor(t *testing.T) {
	model := New("/tmp/does-not-exist.sock")
	if model.Interval != DefaultInterval {
		t.Errorf("Interval = %s, want %s", model.Interval, DefaultInterval)
	}

	model.Interval = 5 * time.Second
	if got := model.refreshDelay(); got != 5*time.Second {
		t.Errorf("refreshDelay = %s, want 5s", got)
	}

	// Below the floor a collection would queue behind the previous one.
	model.Interval = 100 * time.Millisecond
	if got := model.refreshDelay(); got != DefaultInterval {
		t.Errorf("refreshDelay = %s for a sub-second interval, want %s", got, DefaultInterval)
	}
}
