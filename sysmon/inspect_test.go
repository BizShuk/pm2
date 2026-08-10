package sysmon

import (
	"testing"
	"time"

	"github.com/bizshuk/pm2/process"
)

func managedFixture() []process.ProcessInfo {
	return []process.ProcessInfo{
		{
			AppConfig: process.AppConfig{Name: "api", Namespace: "Service", Script: "/srv/run.sh"},
			ID:        1,
			PID:       1978,
			Status:    process.StatusOnline,
			CPU:       12.4,
			Memory:    245760 * 1024,
			StartedAt: time.Now().Add(-time.Hour),
		},
		{
			AppConfig: process.AppConfig{Name: "backup", Script: "/srv/backup.sh"},
			ID:        2,
			Status:    process.StatusStopped,
		},
	}
}

func TestBuildTasksJoinsTreeAndPorts(t *testing.T) {
	procs := parseProcessTable(psOutput)
	ports := map[int][]Port{
		2001: {{PID: 2001, Protocol: "tcp", Address: "0.0.0.0", Port: 8080, State: "LISTEN"}},
		9999: {{PID: 9999, Protocol: "tcp", Address: "*", Port: 1234, State: "LISTEN"}},
	}

	tasks := BuildTasks(managedFixture(), procs, ports)

	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want one per managed application", len(tasks))
	}
	api := tasks[0]
	if api.Namespace != "Service" || api.Name != "api" {
		t.Errorf("identity = %s:%s, want Service:api", api.Namespace, api.Name)
	}
	if len(api.Children) != 2 {
		t.Fatalf("children = %+v, want the whole tree below pid 1978", api.Children)
	}

	// The tree totals are the point of the join: a managed shell script
	// shows near-zero usage while the workers it forked do the work.
	if want := 12.4 + 4.1 + 0.2; api.TreeCPUPercent < want-0.01 || api.TreeCPUPercent > want+0.01 {
		t.Errorf("TreeCPUPercent = %v, want %v", api.TreeCPUPercent, want)
	}
	if want := uint64(245760+88064+12288) * 1024; api.TreeMemoryBytes != want {
		t.Errorf("TreeMemoryBytes = %d, want %d", api.TreeMemoryBytes, want)
	}

	// A child owns the socket, not the root process — looking only at the
	// root PID would report "no ports" for most of what pm2 runs.
	if len(api.Ports) != 1 || api.Ports[0].Port != 8080 {
		t.Errorf("ports = %+v, want the child's listener on 8080 and nothing else", api.Ports)
	}
}

func TestBuildTasksNormalisesEmptyNamespace(t *testing.T) {
	tasks := BuildTasks(managedFixture(), nil, nil)
	if tasks[1].Namespace != process.DefaultNamespace {
		t.Errorf("namespace = %q, want %q", tasks[1].Namespace, process.DefaultNamespace)
	}
}

func TestBuildTasksEmitsEmptySlicesNotNil(t *testing.T) {
	// Children and Ports travel in the emitted JSON, where "null" forces
	// every consumer to special-case an idle task.
	tasks := BuildTasks(managedFixture(), nil, nil)
	for _, task := range tasks {
		if task.Children == nil {
			t.Errorf("%s: Children is nil, want an empty slice", task.Name)
		}
		if task.Ports == nil {
			t.Errorf("%s: Ports is nil, want an empty slice", task.Name)
		}
	}
}

func TestBuildTasksFallsBackToProcessTableMetrics(t *testing.T) {
	// A just-launched application has no daemon sample yet; the OS table
	// already knows what it is using.
	managed := []process.ProcessInfo{{
		AppConfig: process.AppConfig{Name: "fresh"},
		PID:       2001,
		Status:    process.StatusOnline,
	}}

	task := BuildTasks(managed, parseProcessTable(psOutput), nil)[0]
	if task.CPUPercent != 4.1 {
		t.Errorf("CPUPercent = %v, want the process table's 4.1", task.CPUPercent)
	}
	if want := uint64(88064 * 1024); task.MemoryBytes != want {
		t.Errorf("MemoryBytes = %d, want %d", task.MemoryBytes, want)
	}
	if task.Command == "" {
		t.Error("Command is empty; want the process table's command line as a fallback")
	}
}

func TestBuildTasksPrefersDaemonMetrics(t *testing.T) {
	// The daemon samples the managed PID directly and is authoritative;
	// the process table must not overwrite a live reading.
	task := BuildTasks(managedFixture(), parseProcessTable(psOutput), nil)[0]
	if task.CPUPercent != 12.4 {
		t.Errorf("CPUPercent = %v, want the daemon's 12.4", task.CPUPercent)
	}
}

func TestPortsForCombinesRootAndChildren(t *testing.T) {
	ports := map[int][]Port{
		1978: {{PID: 1978, Port: 9000}},
		2001: {{PID: 2001, Port: 8080}},
	}
	children := []Proc{{PID: 2001}}

	got := PortsFor(ports, 1978, children)
	if len(got) != 2 {
		t.Fatalf("got %+v, want both the root's and the child's listeners", got)
	}
	if got[0].Port != 8080 || got[1].Port != 9000 {
		t.Errorf("order = %d,%d, want ascending port order", got[0].Port, got[1].Port)
	}
}

// A managed shell script that execs the real worker owns no GPU time
// itself, exactly as it owns no CPU — so the number that matters is the
// tree total, and per-process GPU has to reach it through the join.
func TestBuildTasksSumsGPUAcrossTheProcessTree(t *testing.T) {
	procs := []Proc{
		{PID: 100, PPID: 1, GPUPercent: 1.5, Command: "/srv/run.sh"},
		{PID: 200, PPID: 100, GPUPercent: 40, Command: "/bin/renderer"},
		{PID: 300, PPID: 200, GPUPercent: 8.5, Command: "/bin/helper"},
		{PID: 999, PPID: 1, GPUPercent: 77, Command: "/unrelated"},
	}
	managed := []process.ProcessInfo{{
		AppConfig: process.AppConfig{Name: "api"},
		ID:        1,
		PID:       100,
	}}

	tasks := BuildTasks(managed, procs, nil)
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(tasks))
	}
	if tasks[0].GPUPercent != 1.5 {
		t.Errorf("GPUPercent = %v, want the root process's own 1.5", tasks[0].GPUPercent)
	}
	if tasks[0].TreeGPUPercent != 50 {
		t.Errorf("TreeGPUPercent = %v, want 50 (1.5 + 40 + 8.5, excluding the unrelated process)", tasks[0].TreeGPUPercent)
	}
}

func TestMergeProcessGPUMapsReadingsOntoTheProcessTable(t *testing.T) {
	procs := []Proc{{PID: 100}, {PID: 200}}
	mergeProcessGPU(procs, &GPU{Processes: []ProcGPU{
		{PID: 200, MillisecondsPerSecond: 456.7},
	}})

	if procs[0].GPUPercent != 0 {
		t.Errorf("unlisted process got %v, want 0", procs[0].GPUPercent)
	}
	// 1000 ms/s is one whole GPU-second per second, so the reported unit
	// scales to the same 0-100 range CPUPercent uses.
	if got := procs[1].GPUPercent; got < 45.66 || got > 45.68 {
		t.Errorf("GPUPercent = %v, want 456.7 ms/s scaled to 45.67", got)
	}
}

// A machine with no agent must not have every process silently rewritten.
func TestMergeProcessGPUIsANoOpWithoutAReading(t *testing.T) {
	procs := []Proc{{PID: 100, GPUPercent: 12}}
	mergeProcessGPU(procs, nil)

	if procs[0].GPUPercent != 12 {
		t.Errorf("GPUPercent = %v, want the table left untouched", procs[0].GPUPercent)
	}
}
