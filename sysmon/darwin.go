package sysmon

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// darwinSampler reads macOS through the base-system CLI tools. There is no
// cgo here on purpose: pm2 ships as a single static binary, so every
// reading has to come from a command or a sysctl.
//
// The CPU number comes from `iostat -c 2 -w 1` rather than `top -l 1`.
// Both report whole-machine utilisation, but iostat's second sample is a
// true one-second delta while top's first sample is skewed by everything
// since boot — and, more importantly, top burns ~0.7 s of system time
// walking the process table on every call, which is a lot of CPU to spend
// on measuring CPU. iostat sleeps for its second and costs ~5 ms, and it
// returns disk throughput and load average in the same output.
type darwinSampler struct {
	network *rateTracker
}

func newDarwinSampler() *darwinSampler {
	return &darwinSampler{network: newRateTracker()}
}

func (d *darwinSampler) sample() (System, error) {
	var failures []error

	system := System{}
	if cpu, load, diskIO, err := d.readIOStat(); err != nil {
		failures = append(failures, err)
	} else {
		system.CPU, system.Load, system.DiskIO = cpu, load, diskIO
	}

	memory, err := d.readMemory()
	if err != nil {
		failures = append(failures, err)
	} else {
		system.Memory = memory
	}

	network, err := d.readNetwork(time.Now())
	if err != nil {
		failures = append(failures, err)
	} else {
		system.Network = network
	}

	disks, err := readDisks()
	if err != nil {
		failures = append(failures, err)
	} else {
		system.Disks = disks
	}

	system.CPU.Cores = runtime.NumCPU()
	return system, errors.Join(failures...)
}

func (d *darwinSampler) host() (Host, error) {
	values, err := readSysctl("hw.ncpu", "kern.boottime")
	if err != nil {
		return Host{Cores: runtime.NumCPU()}, err
	}

	host := Host{Cores: runtime.NumCPU()}
	if cores, convErr := strconv.Atoi(strings.TrimSpace(values["hw.ncpu"])); convErr == nil && cores > 0 {
		host.Cores = cores
	}
	if bootedAt, ok := parseBootTime(values["kern.boottime"]); ok {
		host.UptimeSeconds = int64(time.Since(bootedAt).Seconds())
	}
	return host, nil
}

func (d *darwinSampler) readIOStat() (CPU, Load, DiskIO, error) {
	output, err := exec.Command("iostat", "-c", "2", "-w", "1").Output()
	if err != nil {
		return CPU{}, Load{}, DiskIO{}, fmt.Errorf("read cpu and disk io: %w", err)
	}
	cpu, load, diskIO, ok := parseIOStat(string(output))
	if !ok {
		return CPU{}, Load{}, DiskIO{}, errors.New("read cpu and disk io: unexpected iostat output")
	}
	return cpu, load, diskIO, nil
}

// parseIOStat reads the interval sample out of `iostat -c 2 -w 1`:
//
//	           disk0               disk6       cpu    load average
//	 KB/t  tps  MB/s     KB/t  tps  MB/s  us sy id   1m   5m   15m
//	24.11  307  7.22    42.41   13  0.52   7 13 80  3.08 5.91 6.34
//	20.08 10780 211.35     0.00    0  0.00   7 15 78  3.08 5.91 6.34
//
// The first data row covers everything since boot and the last one covers
// the requested one-second window, so only the last row is used. The disk
// count is derived from the row width — three columns per device plus
// three CPU and three load columns — because the device names live in a
// header whose alignment cannot be relied on.
func parseIOStat(output string) (CPU, Load, DiskIO, bool) {
	var last []string
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		if _, err := strconv.ParseFloat(fields[0], 64); err != nil {
			continue
		}
		last = fields
	}
	if last == nil || (len(last)-6)%3 != 0 {
		return CPU{}, Load{}, DiskIO{}, false
	}

	devices := (len(last) - 6) / 3
	diskIO := DiskIO{}
	for device := range devices {
		transfers, _ := strconv.ParseFloat(last[device*3+1], 64)
		megabytes, _ := strconv.ParseFloat(last[device*3+2], 64)
		diskIO.TransfersPerSecond += transfers
		diskIO.BytesPerSecond += megabytes * 1024 * 1024
	}

	user, _ := strconv.ParseFloat(last[devices*3], 64)
	system, _ := strconv.ParseFloat(last[devices*3+1], 64)
	idle, _ := strconv.ParseFloat(last[devices*3+2], 64)
	one, _ := strconv.ParseFloat(last[devices*3+3], 64)
	five, _ := strconv.ParseFloat(last[devices*3+4], 64)
	fifteen, _ := strconv.ParseFloat(last[devices*3+5], 64)

	cpu := CPU{
		UsedPercent: user + system,
		UserPercent: user,
		SysPercent:  system,
		IdlePercent: idle,
	}
	return cpu, Load{One: one, Five: five, Fifteen: fifteen}, diskIO, true
}

func (d *darwinSampler) readMemory() (Memory, error) {
	pages, err := exec.Command("vm_stat").Output()
	if err != nil {
		return Memory{}, fmt.Errorf("read memory: %w", err)
	}
	values, err := readSysctl("hw.memsize", "vm.swapusage")
	if err != nil {
		return Memory{}, fmt.Errorf("read memory: %w", err)
	}

	total, err := strconv.ParseUint(strings.TrimSpace(values["hw.memsize"]), 10, 64)
	if err != nil {
		return Memory{}, fmt.Errorf("read memory: parse hw.memsize: %w", err)
	}

	memory := parseVMStat(string(pages), total)
	memory.SwapTotalBytes, memory.SwapUsedBytes = parseSwapUsage(values["vm.swapusage"])
	return memory, nil
}

// parseVMStat derives occupancy from `vm_stat` page counts.
//
// Used is everything that is not free or speculative — the definition
// Activity Monitor and `top`'s PhysMem line use, which is why macOS
// habitually reports 95%+ in use. Available adds the inactive and
// purgeable pages the kernel would hand back on demand, so a renderer can
// pair the alarming percentage with the headroom that actually exists.
func parseVMStat(output string, total uint64) Memory {
	pageSize := uint64(4096)
	if _, after, found := strings.Cut(output, "page size of "); found {
		if size, err := strconv.ParseUint(strings.Fields(after)[0], 10, 64); err == nil && size > 0 {
			pageSize = size
		}
	}

	pages := make(map[string]uint64, 8)
	for line := range strings.SplitSeq(output, "\n") {
		label, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		count, err := strconv.ParseUint(strings.Trim(strings.TrimSpace(value), "."), 10, 64)
		if err == nil {
			pages[strings.TrimSpace(label)] = count
		}
	}

	free := (pages["Pages free"] + pages["Pages speculative"]) * pageSize
	reclaimable := (pages["Pages inactive"] + pages["Pages purgeable"]) * pageSize

	memory := Memory{TotalBytes: total, UsedBytes: total - min(free, total)}
	memory.UsedPercent = percent(memory.UsedBytes, total)
	memory.AvailableBytes = min(free+reclaimable, total)
	return memory
}

// parseSwapUsage reads the `vm.swapusage` sysctl:
//
//	total = 11264.00M  used = 9910.50M  free = 1353.50M  (encrypted)
func parseSwapUsage(value string) (total uint64, used uint64) {
	fields := strings.Fields(value)
	for index, field := range fields {
		if index+2 >= len(fields) || fields[index+1] != "=" {
			continue
		}
		switch field {
		case "total":
			total = parseByteSize(fields[index+2])
		case "used":
			used = parseByteSize(fields[index+2])
		}
	}
	return total, used
}

// parseByteSize converts a "9910.50M" style value into bytes. macOS tools
// print binary units without the "i", so M means MiB.
func parseByteSize(value string) uint64 {
	if value == "" {
		return 0
	}
	multiplier := uint64(1)
	switch value[len(value)-1] {
	case 'K', 'k':
		multiplier = 1 << 10
	case 'M', 'm':
		multiplier = 1 << 20
	case 'G', 'g':
		multiplier = 1 << 30
	case 'T', 't':
		multiplier = 1 << 40
	}
	if multiplier > 1 {
		value = value[:len(value)-1]
	}
	size, err := strconv.ParseFloat(value, 64)
	if err != nil || size < 0 {
		return 0
	}
	return uint64(size * float64(multiplier))
}

func (d *darwinSampler) readNetwork(now time.Time) (Network, error) {
	output, err := exec.Command("netstat", "-ib").Output()
	if err != nil {
		return Network{}, fmt.Errorf("read network throughput: %w", err)
	}
	return networkFrom(d.network, parseNetstatInterfaces(string(output)), now), nil
}

// parseNetstatInterfaces reads the per-link rows of `netstat -ib`:
//
//	Name  Mtu   Network     Address            Ipkts Ierrs  Ibytes  Opkts Oerrs  Obytes Coll
//	en5   1500  <Link#8>    a4:83:e7:1c:2b:04  290197  0  1575192126  324460  0  2026000  0
//
// Only `<Link#N>` rows are counted; the per-address rows that follow
// repeat the same totals and would double every number. Rows with an
// address column are one field wider than rows without one, so the seven
// counters are taken from the end of the row rather than by index.
// Loopback is excluded — a dashboard that counts local traffic as network
// throughput is measuring itself.
func parseNetstatInterfaces(output string) []interfaceCounters {
	var counters []interfaceCounters
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 || !strings.HasPrefix(fields[2], "<Link#") {
			continue
		}
		name := strings.TrimSuffix(fields[0], "*")
		if strings.HasPrefix(name, "lo") {
			continue
		}
		tail := fields[len(fields)-7:]
		rx, rxErr := strconv.ParseUint(tail[2], 10, 64)
		tx, txErr := strconv.ParseUint(tail[5], 10, 64)
		if rxErr != nil || txErr != nil {
			continue
		}
		counters = append(counters, interfaceCounters{name: name, rx: rx, tx: tx})
	}
	return counters
}

// readSysctl runs one `sysctl` for every requested OID and returns the
// values keyed by name. Without -n each line is "key: value", so a
// missing or renamed OID drops out of the map instead of silently
// shifting every following value by one line.
func readSysctl(names ...string) (map[string]string, error) {
	output, err := exec.Command("sysctl", names...).Output()
	if err != nil && len(output) == 0 {
		return nil, fmt.Errorf("read sysctl: %w", err)
	}
	values := make(map[string]string, len(names))
	for line := range strings.SplitSeq(string(output), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		values[key] = strings.TrimSpace(value)
	}
	return values, nil
}

// parseBootTime reads the `kern.boottime` sysctl value:
//
//	{ sec = 1785701401, usec = 455017 } Mon Aug  3 04:10:01 2026
func parseBootTime(value string) (time.Time, bool) {
	_, after, found := strings.Cut(value, "sec = ")
	if !found {
		return time.Time{}, false
	}
	digits := strings.TrimFunc(strings.Fields(after)[0], func(r rune) bool {
		return r < '0' || r > '9'
	})
	seconds, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || seconds <= 0 {
		return time.Time{}, false
	}
	return time.Unix(seconds, 0), true
}
