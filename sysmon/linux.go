package sysmon

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// linuxSectorBytes is the fixed 512-byte unit /proc/diskstats counts in,
// regardless of the device's real sector size.
const linuxSectorBytes = 512

// linuxSampler reads everything from /proc, so it works inside a
// container and needs no external binaries beyond `df`.
//
// CPU is a delta between consecutive /proc/stat reads: the file holds
// counters since boot, so the first sample after construction has no
// baseline and reports zero rather than "average utilisation since the
// machine turned on".
type linuxSampler struct {
	network *rateTracker
	disk    *rateTracker

	previousIdle  uint64
	previousTotal uint64
	warmed        bool
}

func newLinuxSampler() *linuxSampler {
	return &linuxSampler{network: newRateTracker(), disk: newRateTracker()}
}

func (l *linuxSampler) sample() (System, error) {
	var failures []error
	now := time.Now()
	system := System{}

	cpu, err := l.readCPU()
	if err != nil {
		failures = append(failures, err)
	} else {
		system.CPU = cpu
	}

	memory, err := readProcMemory()
	if err != nil {
		failures = append(failures, err)
	} else {
		system.Memory = memory
	}

	load, err := readProcLoad()
	if err != nil {
		failures = append(failures, err)
	} else {
		system.Load = load
	}

	network, err := l.readNetwork(now)
	if err != nil {
		failures = append(failures, err)
	} else {
		system.Network = network
	}

	diskIO, err := l.readDiskIO(now)
	if err != nil {
		failures = append(failures, err)
	} else {
		system.DiskIO = diskIO
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

func (l *linuxSampler) host() (Host, error) {
	host := Host{Cores: runtime.NumCPU()}
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return host, fmt.Errorf("read uptime: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return host, errors.New("read uptime: unexpected /proc/uptime format")
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return host, fmt.Errorf("read uptime: %w", err)
	}
	host.UptimeSeconds = int64(seconds)
	return host, nil
}

func (l *linuxSampler) readCPU() (CPU, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return CPU{}, fmt.Errorf("read cpu: %w", err)
	}
	total, idle, ok := parseProcStat(string(data))
	if !ok {
		return CPU{}, errors.New("read cpu: unexpected /proc/stat format")
	}

	cpu := cpuFromDelta(total, idle, l.previousTotal, l.previousIdle, l.warmed)
	l.previousTotal, l.previousIdle, l.warmed = total, idle, true
	return cpu, nil
}

// cpuFromDelta converts two /proc/stat readings into utilisation.
//
// Without a previous sample there is nothing to subtract, so it reports
// zero rather than the since-boot average — on a machine that idled for a
// week that average would read 3% no matter how busy it is right now. A
// counter that failed to advance (or went backwards after a container
// migration) is treated the same way.
func cpuFromDelta(total, idle, previousTotal, previousIdle uint64, warmed bool) CPU {
	if !warmed || total <= previousTotal {
		return CPU{}
	}
	idlePercent := 100 * float64(idle-previousIdle) / float64(total-previousTotal)
	return CPU{UsedPercent: 100 - idlePercent, IdlePercent: idlePercent}
}

// parseProcStat sums the aggregate "cpu" line of /proc/stat into total
// and idle jiffies. idle covers idle+iowait; everything else counts as
// busy, including irq and steal, so a CPU stolen by the hypervisor is not
// reported as free capacity.
func parseProcStat(data string) (total uint64, idle uint64, ok bool) {
	for line := range strings.SplitSeq(data, "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return 0, 0, false
		}
		for index, field := range fields[1:] {
			value, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				continue
			}
			total += value
			if index == 3 || index == 4 {
				idle += value
			}
		}
		return total, idle, true
	}
	return 0, 0, false
}

// readProcMemory reports used memory as MemTotal-MemAvailable, the
// kernel's own estimate of what a new allocation could not reclaim.
func readProcMemory() (Memory, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return Memory{}, fmt.Errorf("read memory: %w", err)
	}

	values := make(map[string]uint64, 8)
	for line := range strings.SplitSeq(string(data), "\n") {
		label, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}
		if kibibytes, convErr := strconv.ParseUint(fields[0], 10, 64); convErr == nil {
			values[label] = kibibytes * 1024
		}
	}
	if values["MemTotal"] == 0 {
		return Memory{}, errors.New("read memory: no MemTotal in /proc/meminfo")
	}

	memory := Memory{
		TotalBytes:     values["MemTotal"],
		UsedBytes:      values["MemTotal"] - min(values["MemAvailable"], values["MemTotal"]),
		AvailableBytes: min(values["MemAvailable"], values["MemTotal"]),
		SwapTotalBytes: values["SwapTotal"],
		SwapUsedBytes:  values["SwapTotal"] - min(values["SwapFree"], values["SwapTotal"]),
	}
	memory.UsedPercent = percent(memory.UsedBytes, memory.TotalBytes)
	return memory, nil
}

func readProcLoad() (Load, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return Load{}, fmt.Errorf("read load average: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return Load{}, errors.New("read load average: unexpected /proc/loadavg format")
	}
	one, _ := strconv.ParseFloat(fields[0], 64)
	five, _ := strconv.ParseFloat(fields[1], 64)
	fifteen, _ := strconv.ParseFloat(fields[2], 64)
	return Load{One: one, Five: five, Fifteen: fifteen}, nil
}

func (l *linuxSampler) readNetwork(now time.Time) (Network, error) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return Network{}, fmt.Errorf("read network throughput: %w", err)
	}
	return networkFrom(l.network, parseProcNetDev(string(data)), now), nil
}

// parseProcNetDev reads the per-interface counters of /proc/net/dev:
//
//	eth0: 1575192126 5128706 0 0 0 0 0 0 2026000 324460 0 0 0 0 0 0
//
// Receive bytes are the first counter and transmit bytes the ninth.
// Loopback is skipped for the same reason as on macOS.
func parseProcNetDev(data string) []interfaceCounters {
	var counters []interfaceCounters
	for line := range strings.SplitSeq(data, "\n") {
		name, values, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" || strings.HasPrefix(name, "lo") {
			continue
		}
		fields := strings.Fields(values)
		if len(fields) < 9 {
			continue
		}
		rx, rxErr := strconv.ParseUint(fields[0], 10, 64)
		tx, txErr := strconv.ParseUint(fields[8], 10, 64)
		if rxErr != nil || txErr != nil {
			continue
		}
		counters = append(counters, interfaceCounters{name: name, rx: rx, tx: tx})
	}
	return counters
}

func (l *linuxSampler) readDiskIO(now time.Time) (DiskIO, error) {
	data, err := os.ReadFile("/proc/diskstats")
	if err != nil {
		return DiskIO{}, fmt.Errorf("read disk io: %w", err)
	}

	return diskIOFrom(l.disk, parseDiskStats(string(data)), now), nil
}

// diskIOFrom turns cumulative per-device counters into throughput. Linux
// is the platform that separates read from write, so ReadWriteSplit is
// always set here — that flag is what tells a renderer to draw two arrows
// instead of one combined figure.
func diskIOFrom(tracker *rateTracker, devices []diskCounters, now time.Time) DiskIO {
	diskIO := DiskIO{ReadWriteSplit: true}
	for _, device := range devices {
		diskIO.ReadBytesPerSecond += tracker.rate(device.name+"/read", device.readBytes, now)
		diskIO.WriteBytesPerSecond += tracker.rate(device.name+"/write", device.writeBytes, now)
		diskIO.TransfersPerSecond += tracker.rate(device.name+"/transfers", device.transfers, now)
	}
	diskIO.BytesPerSecond = diskIO.ReadBytesPerSecond + diskIO.WriteBytesPerSecond
	return diskIO
}

// diskCounters is one block device's cumulative I/O totals.
type diskCounters struct {
	name       string
	readBytes  uint64
	writeBytes uint64
	transfers  uint64
}

// parseDiskStats reads /proc/diskstats, keeping only whole devices.
// Partitions are excluded because their I/O is also counted against the
// parent device, and summing both would double every rate. Virtual
// devices (loop, ram, zram) are excluded because their throughput is
// memory traffic wearing a block device's clothes.
func parseDiskStats(data string) []diskCounters {
	type row struct {
		name                              string
		reads, writes, sectorsR, sectorsW uint64
	}
	var rows []row
	names := make(map[string]bool)

	for line := range strings.SplitSeq(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		name := fields[2]
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "zram") {
			continue
		}
		reads, readsErr := strconv.ParseUint(fields[3], 10, 64)
		sectorsRead, sectorsReadErr := strconv.ParseUint(fields[5], 10, 64)
		writes, writesErr := strconv.ParseUint(fields[7], 10, 64)
		sectorsWritten, sectorsWrittenErr := strconv.ParseUint(fields[9], 10, 64)
		if readsErr != nil || sectorsReadErr != nil || writesErr != nil || sectorsWrittenErr != nil {
			continue
		}
		names[name] = true
		rows = append(rows, row{name: name, reads: reads, writes: writes, sectorsR: sectorsRead, sectorsW: sectorsWritten})
	}

	devices := make([]diskCounters, 0, len(rows))
	for _, entry := range rows {
		if isPartition(entry.name, names) {
			continue
		}
		devices = append(devices, diskCounters{
			name:       entry.name,
			readBytes:  entry.sectorsR * linuxSectorBytes,
			writeBytes: entry.sectorsW * linuxSectorBytes,
			transfers:  entry.reads + entry.writes,
		})
	}
	return devices
}

// isPartition reports whether name is a slice of another listed device:
// "sda1" under "sda", "nvme0n1p3" under "nvme0n1". Both spellings end in
// digits (optionally after a "p"), which is what separates a partition
// from a whole device with a digit in its name such as "nvme0n1".
func isPartition(name string, names map[string]bool) bool {
	for cut := len(name) - 1; cut > 0; cut-- {
		if name[cut] < '0' || name[cut] > '9' {
			break
		}
		parent := name[:cut]
		if parent, found := strings.CutSuffix(parent, "p"); found && names[parent] {
			return true
		}
		if names[parent] {
			return true
		}
	}
	return false
}
