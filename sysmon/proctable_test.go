package sysmon

import "testing"

const psOutput = `    1     0   0.1   6864 Ss   /sbin/launchd
   96     1   1.1  10688 Ss   /usr/libexec/logd
 1978     1  12.4 245760 R    /usr/local/bin/node server.js --port 8080
 2001  1978   4.1  88064 S    /usr/local/bin/node worker.js
 2002  2001   0.2  12288 S    /bin/sh -c sleep 1
malformed row
`

func TestParseProcessTable(t *testing.T) {
	procs := parseProcessTable(psOutput)

	if len(procs) != 5 {
		t.Fatalf("got %d rows, want 5 — the malformed line should be skipped, not fatal", len(procs))
	}
	server := procs[2]
	if server.PID != 1978 || server.PPID != 1 {
		t.Errorf("pid/ppid = %d/%d, want 1978/1", server.PID, server.PPID)
	}
	if server.CPUPercent != 12.4 {
		t.Errorf("CPUPercent = %v, want 12.4", server.CPUPercent)
	}
	if want := uint64(245760 * 1024); server.MemoryBytes != want {
		t.Errorf("MemoryBytes = %d, want %d — ps reports RSS in KiB", server.MemoryBytes, want)
	}
	if server.Command != "/usr/local/bin/node server.js --port 8080" {
		t.Errorf("Command = %q, want the full command line including arguments", server.Command)
	}
}

func TestProcHelpers(t *testing.T) {
	proc := Proc{State: "R", Command: "/usr/local/bin/node server.js"}
	if proc.Executable() != "node" {
		t.Errorf("Executable() = %q, want node", proc.Executable())
	}
	if !proc.Running() {
		t.Error("Running() = false for state R")
	}
	if (Proc{State: "Ss"}).Running() {
		t.Error("Running() = true for a sleeping process")
	}
	if (Proc{}).Executable() != "" {
		t.Error("Executable() should be empty for a row with no command")
	}
}

func TestCountProcesses(t *testing.T) {
	counts := countProcesses(parseProcessTable(psOutput))
	if counts.Total != 5 {
		t.Errorf("Total = %d, want 5", counts.Total)
	}
	if counts.Running != 1 {
		t.Errorf("Running = %d, want 1", counts.Running)
	}
}

func TestDescendantsWalksTheWholeTree(t *testing.T) {
	// 2002 hangs off 2001, which hangs off 1978: a task's real workers are
	// routinely grandchildren, so a one-level lookup would miss them.
	children := Descendants(parseProcessTable(psOutput), 1978)

	if len(children) != 2 {
		t.Fatalf("got %d descendants, want 2001 and its child 2002", len(children))
	}
	if children[0].PID != 2001 || children[1].PID != 2002 {
		t.Errorf("order = %d,%d, want breadth-first 2001 then 2002", children[0].PID, children[1].PID)
	}
}

func TestDescendantsTerminatesOnACycle(t *testing.T) {
	// A table captured mid-reparent can name a process as its own
	// ancestor. Without the visited set this walk never returns.
	procs := []Proc{
		{PID: 10, PPID: 11},
		{PID: 11, PPID: 10},
	}
	children := Descendants(procs, 10)
	if len(children) != 1 || children[0].PID != 11 {
		t.Fatalf("got %+v, want the cycle broken after one hop", children)
	}
}

func TestDescendantsOfUnstartedProcess(t *testing.T) {
	if got := Descendants(parseProcessTable(psOutput), 0); got != nil {
		t.Errorf("got %+v, want nil for a task with no PID", got)
	}
}
