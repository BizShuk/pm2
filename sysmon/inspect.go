package sysmon

import (
	"sort"
	"time"

	"github.com/bizshuk/pm2/process"
)

// Observation is one collection pass: the finished snapshot plus the raw
// OS process table and port map it was assembled from.
//
// The raw halves are kept because the dashboard needs both views of the
// same instant — the managed-task summary and the whole-machine process
// list — and sampling twice would show two different moments a second
// apart.
type Observation struct {
	Snapshot Snapshot
	Procs    []Proc
	Ports    map[int][]Port
}

// Snapshot takes one complete observation and returns only the snapshot.
func (c *Collector) Snapshot(managed []process.ProcessInfo) Snapshot {
	return c.Observe(managed).Snapshot
}

// Observe takes one complete observation: the machine, the OS process
// table, and every managed application joined with the sub-processes it
// spawned and the ports that tree listens on.
//
// A failing source degrades to zero values plus an entry in
// Snapshot.Errors. Returning a partial observation is deliberate: losing
// the port list should not also cost the caller its CPU graph.
func (c *Collector) Observe(managed []process.ProcessInfo) Observation {
	snapshot := Snapshot{Time: time.Now()}

	host, err := c.Host()
	if err != nil {
		snapshot.Errors = append(snapshot.Errors, err.Error())
	}
	snapshot.Host = host

	system, err := c.Sample()
	if err != nil {
		snapshot.Errors = append(snapshot.Errors, err.Error())
	}
	snapshot.System = system

	procs, err := c.Processes()
	if err != nil {
		snapshot.Errors = append(snapshot.Errors, err.Error())
	}
	snapshot.Processes = countProcesses(procs)

	ports, err := c.ListeningPorts()
	if err != nil {
		snapshot.Errors = append(snapshot.Errors, err.Error())
	}

	snapshot.Tasks = BuildTasks(managed, procs, ports)
	return Observation{Snapshot: snapshot, Procs: procs, Ports: ports}
}

// BuildTasks joins managed applications with the OS process table and the
// listening-port map. Exported so a caller holding its own already-taken
// samples (the TUI refreshes those on different cadences) can rebuild the
// task view without re-reading the machine.
func BuildTasks(managed []process.ProcessInfo, procs []Proc, ports map[int][]Port) []Task {
	byParent := indexByParent(procs)

	tasks := make([]Task, 0, len(managed))
	for _, info := range managed {
		task := Task{
			ID:          info.ID,
			Namespace:   process.NamespaceOrDefault(info.Namespace),
			Name:        info.Name,
			Status:      string(info.Status),
			PID:         info.PID,
			Restarts:    info.Restarts,
			CPUPercent:  info.CPU,
			MemoryBytes: info.Memory,
			Command:     info.Script,
			StartedAt:   info.StartedAt,
		}

		// The daemon's own CPU/memory reading is authoritative for the
		// task's root process; the process table only fills it in when the
		// daemon has not sampled yet (a just-launched application).
		if self, found := findProc(procs, info.PID); found {
			if task.CPUPercent == 0 {
				task.CPUPercent = self.CPUPercent
			}
			if task.MemoryBytes == 0 {
				task.MemoryBytes = self.MemoryBytes
			}
			if task.Command == "" {
				task.Command = self.Command
			}
		}

		// Children and Ports are always non-nil: the emitted JSON is a
		// documented wire format, and "[]" spares every consumer a null
		// check that only fires for idle tasks.
		task.Children = append([]Proc{}, descendants(byParent, info.PID)...)
		task.TreeCPUPercent = task.CPUPercent
		task.TreeMemoryBytes = task.MemoryBytes
		for _, child := range task.Children {
			task.TreeCPUPercent += child.CPUPercent
			task.TreeMemoryBytes += child.MemoryBytes
		}
		task.Ports = append([]Port{}, collectPorts(ports, info.PID, task.Children)...)
		tasks = append(tasks, task)
	}
	return tasks
}

// Descendants returns every process below pid in a process table. It is
// the exported entry point for a caller browsing arbitrary OS processes
// rather than managed tasks, which already carry their children.
func Descendants(procs []Proc, pid int) []Proc {
	return descendants(indexByParent(procs), pid)
}

// PortsFor returns the listening sockets owned by pid or any of children.
func PortsFor(ports map[int][]Port, pid int, children []Proc) []Port {
	return collectPorts(ports, pid, children)
}

// collectPorts gathers the listeners owned by a task's root process and
// every descendant. A managed shell script that execs a real server owns
// no socket itself, so looking only at the root PID would report "no
// ports" for most of what pm2 runs.
func collectPorts(ports map[int][]Port, rootPID int, children []Proc) []Port {
	if len(ports) == 0 {
		return nil
	}
	collected := append([]Port(nil), ports[rootPID]...)
	for _, child := range children {
		collected = append(collected, ports[child.PID]...)
	}
	sort.SliceStable(collected, func(i, j int) bool {
		if collected[i].Port != collected[j].Port {
			return collected[i].Port < collected[j].Port
		}
		return collected[i].Address < collected[j].Address
	})
	return collected
}
