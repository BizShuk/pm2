package sysmon

import "testing"

// Captured from `df -Pk` on macOS, where an APFS install mounts half a
// dozen housekeeping volumes that all report the same container size.
const dfOutput = `Filesystem     1024-blocks      Used Available Capacity  Mounted on
/dev/disk3s1s1   239362496  12274816  37183328    25%    /
devfs                  248       248         0   100%    /dev
/dev/disk3s6     239362496  10486048  37183328    22%    /System/Volumes/VM
/dev/disk3s2     239362496  10507000  37183328    23%    /System/Volumes/Preboot
/dev/disk3s5     239362496 166254080  37183328    82%    /System/Volumes/Data
/dev/disk6s2     976598496 300123456 600000000    34%    /Volumes/action
map auto_home            0         0         0   100%    /System/Volumes/Data/home
`

func TestParseDiskUsageKeepsOnlyMeaningfulVolumes(t *testing.T) {
	disks := parseDiskUsage(dfOutput)

	mounts := make([]string, 0, len(disks))
	for _, disk := range disks {
		mounts = append(mounts, disk.Mount)
	}
	if len(disks) != 3 {
		t.Fatalf("got mounts %v, want /, /Volumes/action and /System/Volumes/Data only", mounts)
	}
	// "/" leads regardless of size so the renderer's "first N" is stable.
	if mounts[0] != "/" {
		t.Errorf("first mount = %q, want / to lead", mounts[0])
	}
	if mounts[1] != "/Volumes/action" {
		t.Errorf("second mount = %q, want the largest remaining volume", mounts[1])
	}
}

func TestParseDiskUsageConvertsBlocksToBytes(t *testing.T) {
	disks := parseDiskUsage(dfOutput)

	root := disks[0]
	if want := uint64(239362496 * 1024); root.TotalBytes != want {
		t.Errorf("TotalBytes = %d, want %d — df -Pk reports 1024-byte blocks", root.TotalBytes, want)
	}
	if want := uint64(12274816 * 1024); root.UsedBytes != want {
		t.Errorf("UsedBytes = %d, want %d", root.UsedBytes, want)
	}
	if root.UsedPercent < 5.1 || root.UsedPercent > 5.2 {
		t.Errorf("UsedPercent = %v, want ~5.13", root.UsedPercent)
	}
}

func TestIsPlumbingMount(t *testing.T) {
	cases := map[string]bool{
		"/":                          false,
		"/System/Volumes/Data":       false,
		"/System/Volumes/VM":         true,
		"/System/Volumes/iSCPreboot": true,
		"/Volumes/action":            false,
	}
	for mount, want := range cases {
		if got := isPlumbingMount(mount); got != want {
			t.Errorf("isPlumbingMount(%q) = %v, want %v", mount, got, want)
		}
	}
}

func TestPercentGuardsEmptyDevice(t *testing.T) {
	if got := percent(0, 0); got != 0 {
		t.Errorf("percent(0,0) = %v, want 0 rather than NaN", got)
	}
}
