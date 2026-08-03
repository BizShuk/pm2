package sysmon

import (
	"testing"
	"time"
)

const linuxProcStat = `cpu  100 20 50 800 30 0 0 0 0 0
cpu0 50 10 25 400 15 0 0 0 0 0
intr 12345
`

func TestParseProcStatCountsIdleAndIOWaitAsIdle(t *testing.T) {
	total, idle, ok := parseProcStat(linuxProcStat)
	if !ok {
		t.Fatal("parseProcStat rejected a valid /proc/stat")
	}
	if want := uint64(100 + 20 + 50 + 800 + 30); total != want {
		t.Errorf("total = %d, want %d", total, want)
	}
	if want := uint64(800 + 30); idle != want {
		t.Errorf("idle = %d, want idle+iowait = %d", idle, want)
	}
}

func TestCPUFromDelta(t *testing.T) {
	cases := []struct {
		name                             string
		total, idle, prevTotal, prevIdle uint64
		warmed                           bool
		wantUsed                         float64
	}{
		{name: "first sample has no baseline", total: 1000, idle: 800, warmed: false, wantUsed: 0},
		{name: "half the jiffies were idle", total: 2000, idle: 1400, prevTotal: 1000, prevIdle: 900, warmed: true, wantUsed: 50},
		{name: "fully idle window", total: 2000, idle: 1900, prevTotal: 1000, prevIdle: 900, warmed: true, wantUsed: 0},
		{name: "counter did not advance", total: 1000, idle: 900, prevTotal: 1000, prevIdle: 900, warmed: true, wantUsed: 0},
		{name: "counter went backwards", total: 500, idle: 400, prevTotal: 1000, prevIdle: 900, warmed: true, wantUsed: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cpuFromDelta(tc.total, tc.idle, tc.prevTotal, tc.prevIdle, tc.warmed)
			if got.UsedPercent != tc.wantUsed {
				t.Errorf("UsedPercent = %v, want %v", got.UsedPercent, tc.wantUsed)
			}
		})
	}
}

const linuxNetDev = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets
    lo: 1575192126 5128706    0    0    0     0          0         0 1575192126 5128706
  eth0: 987654321  290197    0    0    0     0          0         0 123456789   324460
`

func TestParseProcNetDev(t *testing.T) {
	counters := parseProcNetDev(linuxNetDev)
	if len(counters) != 1 {
		t.Fatalf("got %+v, want loopback excluded and eth0 kept", counters)
	}
	if counters[0] != (interfaceCounters{name: "eth0", rx: 987654321, tx: 123456789}) {
		t.Errorf("eth0 = %+v, want rx from field 1 and tx from field 9", counters[0])
	}
}

// Fields: major minor name reads merged sectors_read ms writes merged
// sectors_written ms ...
const linuxDiskStats = `   8       0 sda 1000 0 4000 100 500 0 2000 50 0 0 0
   8       1 sda1 900 0 3600 90 450 0 1800 45 0 0 0
 259       0 nvme0n1 2000 0 8000 200 1000 0 4000 100 0 0 0
 259       1 nvme0n1p1 1900 0 7600 190 950 0 3800 95 0 0 0
   7       0 loop0 10 0 40 1 5 0 20 1 0 0 0
`

func TestParseDiskStatsKeepsWholeDevicesOnly(t *testing.T) {
	devices := parseDiskStats(linuxDiskStats)

	names := make(map[string]bool, len(devices))
	for _, device := range devices {
		names[device.name] = true
	}
	if len(devices) != 2 || !names["sda"] || !names["nvme0n1"] {
		t.Fatalf("got %v, want sda and nvme0n1 only — partitions double-count the parent and loop0 is virtual", names)
	}
	for _, device := range devices {
		if device.name != "sda" {
			continue
		}
		if want := uint64(4000 * linuxSectorBytes); device.readBytes != want {
			t.Errorf("sda readBytes = %d, want %d (sectors × 512)", device.readBytes, want)
		}
		if want := uint64(1000 + 500); device.transfers != want {
			t.Errorf("sda transfers = %d, want reads+writes = %d", device.transfers, want)
		}
	}
}

func TestIsPartition(t *testing.T) {
	names := map[string]bool{"sda": true, "nvme0n1": true, "mmcblk0": true}
	cases := map[string]bool{
		"sda":        false,
		"sda1":       true,
		"sda12":      true,
		"nvme0n1":    false,
		"nvme0n1p3":  true,
		"mmcblk0":    false,
		"mmcblk0p1":  true,
		"unrelated1": false,
	}
	for name, want := range cases {
		if got := isPartition(name, names); got != want {
			t.Errorf("isPartition(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestDiskIOFromSplitsReadAndWrite(t *testing.T) {
	tracker := newRateTracker()
	start := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	first := []diskCounters{{name: "sda", readBytes: 1000, writeBytes: 2000, transfers: 10}}

	if got := diskIOFrom(tracker, first, start); got.BytesPerSecond != 0 {
		t.Errorf("first sample = %v bytes/s, want 0 with no baseline", got.BytesPerSecond)
	}

	second := []diskCounters{{name: "sda", readBytes: 3000, writeBytes: 6000, transfers: 30}}
	got := diskIOFrom(tracker, second, start.Add(2*time.Second))

	if !got.ReadWriteSplit {
		t.Error("ReadWriteSplit = false, want true so renderers show read and write separately")
	}
	if got.ReadBytesPerSecond != 1000 || got.WriteBytesPerSecond != 2000 {
		t.Errorf("read/write = %v/%v bytes/s, want 1000/2000", got.ReadBytesPerSecond, got.WriteBytesPerSecond)
	}
	if got.BytesPerSecond != 3000 {
		t.Errorf("BytesPerSecond = %v, want read+write = 3000", got.BytesPerSecond)
	}
	if got.TransfersPerSecond != 10 {
		t.Errorf("TransfersPerSecond = %v, want 20 transfers over 2s", got.TransfersPerSecond)
	}
}
