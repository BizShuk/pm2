package taskmanager

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/sysmon"
)

// emitTimestampLayout matches logfile's managed-line prefix so text
// snapshots read consistently alongside application logs when both land
// in the same stream.
const emitTimestampLayout = "2006-01-02 15:04:05"

// encodeText writes a snapshot as timestamped key=value lines: one for
// the machine, one per mounted filesystem, one per managed task.
//
// Every line is self-contained and prefixed with `scope=`, so a reader
// can grep for exactly the dimension it cares about without a parser,
// which is what a log stream is good at. The full-fidelity record is the
// JSON format; this one is for humans reading a tail.
func encodeText(out io.Writer, snapshot sysmon.Snapshot) error {
	stamp := snapshot.Time.Format(emitTimestampLayout)
	lines := []string{hostLine(snapshot)}

	for _, disk := range snapshot.System.Disks {
		lines = append(lines, fmt.Sprintf(
			"scope=disk mount=%q used=%s total=%s pct=%.1f",
			disk.Mount,
			process.FormatBytes(disk.UsedBytes),
			process.FormatBytes(disk.TotalBytes),
			disk.UsedPercent,
		))
	}
	for _, task := range snapshot.Tasks {
		lines = append(lines, taskLine(task))
	}
	for _, failure := range snapshot.Errors {
		// Quoted because a collector error is free text and would
		// otherwise spill across the key=value fields around it.
		lines = append(lines, "scope=error message="+strconv.Quote(failure))
	}

	for _, line := range lines {
		if _, err := fmt.Fprintf(out, "[%s] %s\n", stamp, line); err != nil {
			return err
		}
	}
	return nil
}

// hostLine appends the GPU fields rather than interleaving them: they
// are present only while a privileged agent publishes readings, and a
// field that appears in the middle of the line on some machines would
// break every column-counting reader on the others.
func hostLine(snapshot sysmon.Snapshot) string {
	system := snapshot.System
	return fmt.Sprintf(
		"scope=host host=%s cpu=%.1f%% user=%.1f%% sys=%.1f%% load=%.2f/%.2f/%.2f "+
			"mem=%.1f%% mem_used=%s mem_avail=%s swap=%s/%s "+
			"net_if=%s net_rx=%s/s net_tx=%s/s disk_io=%s/s disk_iops=%.0f "+
			"procs=%d running=%d uptime=%ds",
		snapshot.Host.Hostname,
		system.CPU.UsedPercent, system.CPU.UserPercent, system.CPU.SysPercent,
		system.Load.One, system.Load.Five, system.Load.Fifteen,
		system.Memory.UsedPercent,
		process.FormatBytes(system.Memory.UsedBytes),
		process.FormatBytes(system.Memory.AvailableBytes),
		process.FormatBytes(system.Memory.SwapUsedBytes),
		process.FormatBytes(system.Memory.SwapTotalBytes),
		dashIfEmpty(system.Network.Interface),
		process.FormatBytes(uint64(system.Network.RxBytesPerSecond)),
		process.FormatBytes(uint64(system.Network.TxBytesPerSecond)),
		process.FormatBytes(uint64(system.DiskIO.BytesPerSecond)),
		system.DiskIO.TransfersPerSecond,
		snapshot.Processes.Total, snapshot.Processes.Running,
		snapshot.Host.UptimeSeconds,
	) + gpuFields(system.GPU)
}

// gpuFields renders the optional GPU tail, or nothing at all. An absent
// agent leaves the fields out entirely rather than emitting zeros, which
// a reader would happily average into "the GPU is idle".
func gpuFields(gpu *sysmon.GPU) string {
	if gpu == nil {
		return ""
	}
	return fmt.Sprintf(" gpu=%.1f%% gpu_mhz=%.0f gpu_mw=%.0f gpu_age=%.0fs",
		gpu.UtilizationPercent,
		gpu.FrequencyMHz,
		gpu.PowerMilliwatts,
		time.Since(gpu.SampledAt).Seconds(),
	)
}

// taskLine quotes the identity because pm2 application names routinely
// contain spaces ("LLM Proxy"), which would otherwise split one field
// into two and desync every key=value pair after it.
func taskLine(task sysmon.Task) string {
	return fmt.Sprintf(
		"scope=task name=%q status=%s pid=%s cpu=%.1f%% mem=%s "+
			"tree_cpu=%.1f%% tree_mem=%s children=%d ports=%s restarts=%d",
		task.Namespace+":"+task.Name, task.Status,
		process.PIDOrDash(task.PID),
		task.CPUPercent, process.FormatBytes(task.MemoryBytes),
		task.TreeCPUPercent, process.FormatBytes(task.TreeMemoryBytes),
		len(task.Children),
		portList(task.Ports),
		task.Restarts,
	) + taskGPUFields(task)
}

// taskGPUFields appends per-process GPU only where it was measured. A
// task on a machine with no agent, or on hardware that cannot attribute
// GPU time, emits nothing rather than a zero a reader would average in.
func taskGPUFields(task sysmon.Task) string {
	if task.TreeGPUPercent <= 0 {
		return ""
	}
	return fmt.Sprintf(" gpu=%.1f%% tree_gpu=%.1f%%", task.GPUPercent, task.TreeGPUPercent)
}

// portList renders a task's listeners as "tcp/8080,tcp/9229" so the whole
// value stays inside one whitespace-delimited field.
func portList(ports []sysmon.Port) string {
	if len(ports) == 0 {
		return process.Dash
	}
	parts := make([]string, 0, len(ports))
	for _, port := range ports {
		parts = append(parts, fmt.Sprintf("%s/%d", port.Protocol, port.Port))
	}
	return strings.Join(parts, ",")
}

func dashIfEmpty(value string) string {
	if value == "" {
		return process.Dash
	}
	return value
}
