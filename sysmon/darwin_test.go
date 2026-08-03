package sysmon

import (
	"math"
	"testing"
	"time"
)

// Captured from `iostat -c 2 -w 1` on macOS 15 with two block devices.
// The first data row is cumulative since boot; only the second describes
// the requested one-second window.
const darwinIOStat = `              disk0               disk6       cpu    load average
    KB/t  tps  MB/s     KB/t  tps  MB/s  us sy id   1m   5m   15m
   24.11  307  7.22    42.41   13  0.52   7 13 80  3.08 5.91 6.34
   20.08 10780 211.35     0.00    0  0.00   9 15 76  3.10 5.90 6.30
`

func TestParseIOStatUsesIntervalSample(t *testing.T) {
	cpu, load, diskIO, ok := parseIOStat(darwinIOStat)
	if !ok {
		t.Fatal("parseIOStat rejected valid output")
	}

	if cpu.UserPercent != 9 || cpu.SysPercent != 15 || cpu.IdlePercent != 76 {
		t.Errorf("read the wrong row: got user=%v sys=%v idle=%v, want the second sample 9/15/76",
			cpu.UserPercent, cpu.SysPercent, cpu.IdlePercent)
	}
	if cpu.UsedPercent != 24 {
		t.Errorf("UsedPercent = %v, want user+sys = 24", cpu.UsedPercent)
	}
	if load != (Load{One: 3.10, Five: 5.90, Fifteen: 6.30}) {
		t.Errorf("Load = %+v, want the second row's averages", load)
	}
	if diskIO.TransfersPerSecond != 10780 {
		t.Errorf("TransfersPerSecond = %v, want 10780 summed across both devices", diskIO.TransfersPerSecond)
	}
	if want := 211.35 * 1024 * 1024; math.Abs(diskIO.BytesPerSecond-want) > 1 {
		t.Errorf("BytesPerSecond = %v, want %v", diskIO.BytesPerSecond, want)
	}
	if diskIO.ReadWriteSplit {
		t.Error("macOS iostat reports combined throughput; ReadWriteSplit must stay false")
	}
}

func TestParseIOStatRejectsUnexpectedWidth(t *testing.T) {
	// A row that is not 3n+6 fields wide cannot be split into device
	// triples, and guessing would silently mis-assign the CPU columns.
	if _, _, _, ok := parseIOStat("  1.0 2.0 3.0 4.0 5.0 6.0 7.0\n"); ok {
		t.Error("parseIOStat accepted a row whose width fits no device count")
	}
	if _, _, _, ok := parseIOStat("no numeric rows here\n"); ok {
		t.Error("parseIOStat accepted output with no data row")
	}
}

const darwinVMStat = `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                                     4812.
Pages active:                                 149985.
Pages inactive:                               145810.
Pages speculative:                              3354.
Pages throttled:                                   0.
Pages wired down:                             263139.
Pages purgeable:                                  36.
Pages occupied by compressor:                 446273.
`

func TestParseVMStat(t *testing.T) {
	const pageSize = 16384
	total := uint64(16 << 30)

	memory := parseVMStat(darwinVMStat, total)

	// macOS calls everything that is not free or speculative "used".
	wantFree := uint64(4812+3354) * pageSize
	if got, want := memory.UsedBytes, total-wantFree; got != want {
		t.Errorf("UsedBytes = %d, want %d (total minus free+speculative)", got, want)
	}
	// Available adds the pages the kernel would reclaim on demand.
	wantAvailable := wantFree + uint64(145810+36)*pageSize
	if memory.AvailableBytes != wantAvailable {
		t.Errorf("AvailableBytes = %d, want %d", memory.AvailableBytes, wantAvailable)
	}
	if memory.UsedPercent < 90 || memory.UsedPercent > 100 {
		t.Errorf("UsedPercent = %v, want a value in 90-100 for this capture", memory.UsedPercent)
	}
}

func TestParseVMStatFallsBackToDefaultPageSize(t *testing.T) {
	memory := parseVMStat("Pages free: 100.\nPages speculative: 0.\n", 1<<30)
	if want := uint64(1<<30) - 100*4096; memory.UsedBytes != want {
		t.Errorf("UsedBytes = %d, want %d using the 4096-byte default page size", memory.UsedBytes, want)
	}
}

func TestParseSwapUsage(t *testing.T) {
	total, used := parseSwapUsage("total = 11264.00M  used = 9910.50M  free = 1353.50M  (encrypted)")
	if want := uint64(11264 * 1 << 20); total != want {
		t.Errorf("total = %d, want %d", total, want)
	}
	if want := uint64(9910.50 * (1 << 20)); used != want {
		t.Errorf("used = %d, want %d", used, want)
	}
}

func TestParseByteSize(t *testing.T) {
	cases := map[string]uint64{
		"512":     512,
		"1K":      1 << 10,
		"2.5M":    2621440,
		"3G":      3 << 30,
		"":        0,
		"garbage": 0,
	}
	for input, want := range cases {
		if got := parseByteSize(input); got != want {
			t.Errorf("parseByteSize(%q) = %d, want %d", input, got, want)
		}
	}
}

// Captured from `netstat -ib`: en5 carries a MAC address column and lo0
// does not, so the two rows have different field counts.
const darwinNetstat = `Name       Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll
lo0        16384 <Link#1>                       5128706     0 1575192126  5128706     0 1575192126     0
lo0        16384 127           localhost        5128706     - 1575192126  5128706     - 1575192126     -
gif0*      1280  <Link#2>                             0     0          0        0     0          0     0
en5        1500  <Link#8>    a4:83:e7:1c:2b:04   290197     0  987654321   324460     0  123456789     0
en5        1500  192.168.1     192.168.1.109     290197     -  987654321   324460     -  123456789     -
`

func TestParseNetstatInterfacesSkipsLoopbackAndAddressRows(t *testing.T) {
	counters := parseNetstatInterfaces(darwinNetstat)

	if len(counters) != 2 {
		t.Fatalf("got %d interfaces %+v, want gif0 and en5 only (lo0 excluded, address rows deduplicated)",
			len(counters), counters)
	}
	if counters[0].name != "gif0" {
		t.Errorf("first interface = %q, want the trailing * stripped from gif0*", counters[0].name)
	}
	if counters[1] != (interfaceCounters{name: "en5", rx: 987654321, tx: 123456789}) {
		t.Errorf("en5 counters = %+v, want rx/tx read from the last seven fields", counters[1])
	}
}

func TestNetworkFromComputesRatesAndBusiestLink(t *testing.T) {
	tracker := newRateTracker()
	start := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

	first := networkFrom(tracker, []interfaceCounters{
		{name: "en0", rx: 1000, tx: 500},
		{name: "en1", rx: 10, tx: 10},
	}, start)
	if first.RxBytesPerSecond != 0 || first.TxBytesPerSecond != 0 {
		t.Errorf("first sample rates = %+v, want zero — there is no baseline to subtract", first)
	}

	second := networkFrom(tracker, []interfaceCounters{
		{name: "en0", rx: 3000, tx: 1500},
		{name: "en1", rx: 10, tx: 10},
	}, start.Add(2*time.Second))
	if second.RxBytesPerSecond != 1000 {
		t.Errorf("RxBytesPerSecond = %v, want 2000 bytes over 2s", second.RxBytesPerSecond)
	}
	if second.TxBytesPerSecond != 500 {
		t.Errorf("TxBytesPerSecond = %v, want 1000 bytes over 2s", second.TxBytesPerSecond)
	}
	if second.Interface != "en0" {
		t.Errorf("Interface = %q, want the busiest link en0", second.Interface)
	}
	if second.RxBytesTotal != 3010 {
		t.Errorf("RxBytesTotal = %d, want the sum across interfaces", second.RxBytesTotal)
	}
}

func TestParseBootTime(t *testing.T) {
	got, ok := parseBootTime("{ sec = 1785701401, usec = 455017 } Mon Aug  3 04:10:01 2026")
	if !ok {
		t.Fatal("parseBootTime rejected a valid kern.boottime value")
	}
	if got.Unix() != 1785701401 {
		t.Errorf("boot time = %d, want 1785701401", got.Unix())
	}
	if _, ok := parseBootTime("nonsense"); ok {
		t.Error("parseBootTime accepted a value with no sec field")
	}
}
