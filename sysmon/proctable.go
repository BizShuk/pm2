package sysmon

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// processTableArgs is the `ps` invocation per platform. Both forms print
// pid, ppid, %cpu, rss (KiB), state and the full command with no header,
// which keeps one parser for every OS. macOS ps rejects `-e`, Linux procps
// accepts both but `-eo` is the documented spelling there.
func processTableArgs(goos string) []string {
	if goos == "linux" {
		return []string{"-eo", "pid=,ppid=,pcpu=,rss=,stat=,args="}
	}
	return []string{"-axo", "pid=,ppid=,pcpu=,rss=,stat=,command="}
}

// readProcessTable shells out to `ps` once and returns every visible
// process. One call for the whole machine is deliberately cheaper than
// one `ps -p` per managed application: a 650-process table costs ~35 ms,
// while thirty single-PID probes cost several times that.
func readProcessTable(goos string) ([]Proc, error) {
	output, err := exec.Command("ps", processTableArgs(goos)...).Output()
	if err != nil {
		return nil, fmt.Errorf("read process table: %w", err)
	}
	return parseProcessTable(string(output)), nil
}

// parseProcessTable turns headerless `ps` output into Proc rows. Lines
// that do not start with two integers are skipped rather than failing the
// whole table — a single unparsable row must not blank the dashboard.
func parseProcessTable(output string) []Proc {
	var procs []Proc
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		cpu, _ := strconv.ParseFloat(fields[2], 64)
		rss, _ := strconv.ParseUint(fields[3], 10, 64)
		procs = append(procs, Proc{
			PID:         pid,
			PPID:        ppid,
			CPUPercent:  cpu,
			MemoryBytes: rss * 1024,
			State:       fields[4],
			Command:     strings.Join(fields[5:], " "),
		})
	}
	return procs
}

// Executable is the base name of the process's program, e.g. "node" for
// "/usr/local/bin/node server.js". Returns the raw command when it has no
// path separator to strip.
func (p Proc) Executable() string {
	command := strings.TrimSpace(p.Command)
	if command == "" {
		return ""
	}
	first, _, _ := strings.Cut(command, " ")
	return filepath.Base(first)
}

// Running reports whether the process was on a run queue at sample time.
func (p Proc) Running() bool {
	return strings.HasPrefix(p.State, "R")
}

// countProcesses summarises a process table for the snapshot header.
func countProcesses(procs []Proc) ProcCounts {
	counts := ProcCounts{Total: len(procs)}
	for _, proc := range procs {
		if proc.Running() {
			counts.Running++
		}
	}
	return counts
}

// indexByParent groups a process table by parent PID so descendants can
// walk the tree without rescanning the slice per level.
func indexByParent(procs []Proc) map[int][]Proc {
	byParent := make(map[int][]Proc, len(procs))
	for _, proc := range procs {
		byParent[proc.PPID] = append(byParent[proc.PPID], proc)
	}
	return byParent
}

// descendants returns every process below rootPID, breadth-first, so the
// direct children of a managed application come first. Already-visited
// PIDs are skipped: a `ps` table captured mid-reparent can name a process
// as its own ancestor, and an unguarded walk would never terminate.
func descendants(byParent map[int][]Proc, rootPID int) []Proc {
	if rootPID <= 0 {
		return nil
	}
	var (
		found   []Proc
		visited = map[int]bool{rootPID: true}
		queue   = []int{rootPID}
	)
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, child := range byParent[parent] {
			if visited[child.PID] {
				continue
			}
			visited[child.PID] = true
			found = append(found, child)
			queue = append(queue, child.PID)
		}
	}
	return found
}

// findProc returns the row for pid, if the table still holds one.
func findProc(procs []Proc, pid int) (Proc, bool) {
	for _, proc := range procs {
		if proc.PID == pid {
			return proc, true
		}
	}
	return Proc{}, false
}
