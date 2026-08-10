package gpu

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bizshuk/pm2/sysmon"
	"github.com/spf13/cobra"
)

// StatusCmd is `pm2 gpu status` — the unprivileged reader.
//
// It performs exactly the read the daemon and the dashboard perform, so
// it is the one command that answers "is the GPU reading reaching pm2,
// and if not, whose fault is it". Like `pm2 daemon status` it never
// returns an error for the absent case: a machine with no agent
// installed is a normal machine, not a failed command.
var StatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the GPU reading pm2 is currently seeing",
	Long: "Reads the file the privileged agent publishes and prints the\n" +
		"current reading, or why there isn't one. Requires no privileges;\n" +
		"this is the same read `pm2 taskmanager` performs each refresh.",
	Args: cobra.NoArgs,
	RunE: runStatus,
}

var statusPath string

func init() {
	StatusCmd.Flags().StringVar(&statusPath, "path", sysmon.DefaultGPUExportPath,
		"path to read readings from")
}

func runStatus(cmd *cobra.Command, _ []string) error {
	gpu, err := sysmon.ReadGPU(statusPath)
	fmt.Fprint(cmd.OutOrStdout(), formatStatus(statusPath, gpu, err))
	return nil
}

// formatStatus renders the reading, or the most useful diagnosis of its
// absence. It is a pure function of what the read returned so the three
// outcomes an operator actually hits can be tested without a root
// process or a real GPU.
func formatStatus(path string, gpu *sysmon.GPU, err error) string {
	var lines []string
	switch {
	case gpu != nil:
		lines = []string{
			field("status", "publishing"),
			field("source", gpu.Source),
			field("utilization", fmt.Sprintf("%.1f%%", gpu.UtilizationPercent)),
			field("frequency", fmt.Sprintf("%.0f MHz", gpu.FrequencyMHz)),
			field("power", fmt.Sprintf("%.0f mW", gpu.PowerMilliwatts)),
			field("age", time.Since(gpu.SampledAt).Round(time.Millisecond).String()),
			field("interval", fmt.Sprintf("%.1fs", gpu.IntervalSeconds)),
			field("per-process", perProcessSummary(*gpu)),
		}
	case errors.Is(err, os.ErrNotExist):
		lines = []string{
			field("status", "no agent"),
			field("reason", "nothing has published a reading"),
			field("fix", "sudo pm2 gpu install"),
		}
	case errors.Is(err, sysmon.ErrGPUStale):
		lines = []string{
			field("status", "stale"),
			field("reason", err.Error()),
			field("fix", "sudo launchctl kickstart -k system/"+gpuAgentLabel),
		}
	default:
		lines = []string{
			field("status", "unreadable"),
			field("reason", err.Error()),
		}
	}

	lines = append(lines, field("path", path))
	return strings.Join(lines, "\n") + "\n"
}

// perProcessSummary distinguishes hardware that cannot attribute GPU
// time from a machine where nothing used the GPU. Reporting both as
// "none" would send an operator hunting for a process that was never
// measurable in the first place.
func perProcessSummary(gpu sysmon.GPU) string {
	if !gpu.PerProcessSupported {
		return "unsupported on this hardware"
	}
	if len(gpu.Processes) == 0 {
		return "supported; no process used the GPU this sample"
	}

	busiest := gpu.Processes[0]
	for _, entry := range gpu.Processes[1:] {
		if entry.MillisecondsPerSecond > busiest.MillisecondsPerSecond {
			busiest = entry
		}
	}
	return fmt.Sprintf("%d processes; busiest pid %d at %.1f%%",
		len(gpu.Processes), busiest.PID, busiest.GPUPercent())
}

func field(label, value string) string {
	return fmt.Sprintf("%-12s %s", label+":", value)
}
