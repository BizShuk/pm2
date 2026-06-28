package daemon

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/bizshuk/pm2/process"
)

// getProcessMetrics reads %CPU and RSS bytes for pid via `ps`.
// Returns (0, 0) for missing or unreadable processes.
func getProcessMetrics(pid int) (float64, uint64) {
	if pid <= 0 {
		return 0, 0
	}
	out, err := exec.Command("ps", "-p", fmt.Sprintf("%d", pid), "-o", "%cpu,rss").Output()
	if err != nil {
		return 0, 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, 0
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 2 {
		return 0, 0
	}
	var cpu float64
	var rss uint64
	_, _ = fmt.Sscanf(fields[0], "%f", &cpu)
	_, _ = fmt.Sscanf(fields[1], "%d", &rss)
	return cpu, rss * 1024
}

// StartMetricsCollector spawns a goroutine that refreshes CPU/Mem on every
// process every 2 seconds. Runs until the daemon exits.
func (s *Server) StartMetricsCollector() {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			s.mu.Lock()
			for _, mp := range s.processes {
				if mp.Info.PID > 0 && mp.Info.Status == process.StatusOnline {
					cpu, mem := getProcessMetrics(mp.Info.PID)
					mp.Info.CPU = cpu
					mp.Info.Memory = mem
				} else {
					mp.Info.CPU = 0
					mp.Info.Memory = 0
				}
			}
			s.mu.Unlock()
		}
	}()
}