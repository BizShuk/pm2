package sysmon

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// readDisks returns usage for every mounted filesystem backed by a real
// device. `df -Pk` is used rather than statfs(2) because POSIX mode
// guarantees the same six columns on macOS and Linux, while the Statfs_t
// struct differs enough between them to need build-tagged conversions.
func readDisks() ([]Disk, error) {
	output, err := exec.Command("df", "-Pk").Output()
	if err != nil {
		return nil, fmt.Errorf("read filesystem usage: %w", err)
	}
	return parseDiskUsage(string(output)), nil
}

// parseDiskUsage consumes `df -Pk` output:
//
//	Filesystem  1024-blocks  Used  Available  Capacity  Mounted on
//	/dev/disk3s1s1  239362496  12274816  37183328  25%  /
//
// Only device-backed rows are kept, so tmpfs, devfs, overlay and network
// mounts do not crowd out the volumes a user actually cares about. Rows
// are ordered largest-first with "/" always leading, which makes "show
// the first N" a sensible thing for a renderer to do.
func parseDiskUsage(output string) []Disk {
	var disks []Disk
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || !strings.HasPrefix(fields[0], "/dev/") {
			continue
		}
		if isPlumbingMount(strings.Join(fields[5:], " ")) {
			continue
		}
		blocks, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || blocks == 0 {
			continue
		}
		used, err := strconv.ParseUint(fields[2], 10, 64)
		if err != nil {
			continue
		}
		total := blocks * 1024
		usedBytes := used * 1024
		disks = append(disks, Disk{
			Device:      fields[0],
			Mount:       strings.Join(fields[5:], " "),
			TotalBytes:  total,
			UsedBytes:   usedBytes,
			UsedPercent: percent(usedBytes, total),
		})
	}

	sort.SliceStable(disks, func(i, j int) bool {
		if (disks[i].Mount == "/") != (disks[j].Mount == "/") {
			return disks[i].Mount == "/"
		}
		return disks[i].TotalBytes > disks[j].TotalBytes
	})
	return disks
}

// isPlumbingMount reports whether a mount point is OS machinery rather
// than storage a user manages.
//
// An APFS install mounts six or seven volumes under /System/Volumes —
// Preboot, Update, VM, xarts, iSCPreboot, Hardware — that all report the
// same container size and tell nobody anything. Only Data is kept: on
// macOS that is where every user file actually lives, while "/" is the
// sealed read-only system snapshot.
func isPlumbingMount(mount string) bool {
	const appleSystemVolumes = "/System/Volumes/"
	return strings.HasPrefix(mount, appleSystemVolumes) && mount != appleSystemVolumes+"Data"
}

// percent returns part/whole as a percentage, guarding the empty-device
// case that would otherwise produce NaN and render as "NaN%".
func percent(part, whole uint64) float64 {
	if whole == 0 {
		return 0
	}
	return 100 * float64(part) / float64(whole)
}
