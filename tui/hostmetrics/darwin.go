package hostmetrics

import (
	"fmt"
	"os/exec"
	"strings"
)

// darwinCollector reads CPU/Mem percentages from `top -l 1 -n 0`.
// The output format is the legacy macOS `top` (not Linux top) —
// it includes a "CPU usage: X% user, Y% sys" line and a
// "PhysMem: <used> used, <unused> unused" line. We sum user+sys
// into a single CPU percentage (BSD top doesn't expose idle in
// its first sample when -n 0 is used) and convert the used/
// unused values into a used/total ratio.
type darwinCollector struct{}

func newDarwinCollector() *darwinCollector { return &darwinCollector{} }

func (d *darwinCollector) Collect() (float64, float64, error) {
	out, err := exec.Command("top", "-l", "1", "-n", "0").Output()
	if err != nil {
		return 0, 0, err
	}

	var (
		cpu float64
		mem float64
		// We track whether each field was parsed at all so we can
		// return a meaningful error when the format shifts.
		gotCPU bool
		gotMem bool
	)

	for line := range strings.SplitSeq(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "CPU usage:"):
			var user, sys float64
			if _, err := fmt.Sscanf(line, "CPU usage: %f%% user, %f%% sys", &user, &sys); err == nil {
				cpu = user + sys
				gotCPU = true
			}
		case strings.HasPrefix(line, "PhysMem:"):
			parts := strings.Split(line, ",")
			if len(parts) >= 2 {
				var usedVal, unusedVal float64
				var usedUnit, unusedUnit string
				usedStr := strings.TrimPrefix(parts[0], "PhysMem: ")
				_, _ = fmt.Sscanf(usedStr, "%f%s used", &usedVal, &usedUnit)
				unusedStr := strings.TrimSpace(parts[1])
				_, _ = fmt.Sscanf(unusedStr, "%f%s unused", &unusedVal, &unusedUnit)
				if usedVal > 0 && unusedVal > 0 {
					usedBytes := toBytes(usedVal, usedUnit)
					unusedBytes := toBytes(unusedVal, unusedUnit)
					total := usedBytes + unusedBytes
					if total > 0 {
						mem = (float64(usedBytes) / float64(total)) * 100
						gotMem = true
					}
				}
			}
		}
	}

	if !gotCPU || !gotMem {
		return 0, 0, fmt.Errorf("darwin top: missing CPU/PhysMem lines")
	}
	return cpu, mem, nil
}

// toBytes converts a `top`-style "<value> <unit>" pair (e.g. "8 GB")
// into bytes. Mac top emits binary units (GiB, MiB, KiB) so we
// divide by 1024 not 1000.
func toBytes(val float64, unit string) uint64 {
	unit = strings.ToUpper(strings.TrimSpace(unit))
	switch {
	case strings.HasPrefix(unit, "G"):
		return uint64(val * 1024 * 1024 * 1024)
	case strings.HasPrefix(unit, "M"):
		return uint64(val * 1024 * 1024)
	case strings.HasPrefix(unit, "K"):
		return uint64(val * 1024)
	default:
		return uint64(val)
	}
}
