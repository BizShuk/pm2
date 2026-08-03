package dashboard

import (
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

func TestNarrowTerminalDegradesGracefully(t *testing.T) {
	model := loaded(t)
	model.width, model.height = 40, 20
	if frame := model.View(); !strings.Contains(frame, "too narrow") {
		t.Errorf("frame = %q, want the narrow-terminal notice", frame)
	}
}
