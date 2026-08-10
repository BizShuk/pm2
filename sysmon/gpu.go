package sysmon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// DefaultGPUExportPath is where the privileged agent publishes its
// reading and where every unprivileged reader looks for it.
//
// The split exists because `powermetrics` — the only macOS interface
// that reports GPU residency and power — refuses to run as anyone but
// root. Making the pm2 daemon root so it could call the tool itself
// would make every managed task root too (executor.BuildCommand passes
// no Credential), and would hand root ownership of the socket, the
// dump and every log file. Publishing through a file inverts that: one
// small root process writes, everybody else only reads.
//
// /var/run is root-owned and world-readable on both macOS and Linux,
// and is cleared at boot — exactly the lifetime a live metric wants.
const DefaultGPUExportPath = "/var/run/pm2-gpu.json"

// gpuStaleFloor is the shortest age at which a published reading is
// rejected. A reading is also rejected once it is older than three of
// the agent's own intervals, whichever is longer: a fast agent should
// be caught quickly, a deliberately slow one should not be declared
// dead for keeping to its own schedule.
const gpuStaleFloor = 10 * time.Second

// ErrGPUStale reports an export file whose reading is too old to show.
// It is deliberately distinct from a missing file: nothing running and
// something wedged are different problems for the operator.
var ErrGPUStale = errors.New("sysmon: GPU reading is stale")

// GPU is one whole-machine graphics reading, published by the agent and
// consumed by everyone else.
//
// SampledAt and IntervalSeconds ride along in the snapshot on purpose.
// Unlike every other field of System, this one is not measured by the
// process doing the rendering, so a consumer needs to be able to see
// how fresh it is and on whose clock.
type GPU struct {
	Source             string    `json:"source"`
	UtilizationPercent float64   `json:"utilization_percent"`
	FrequencyMHz       float64   `json:"frequency_mhz"`
	PowerMilliwatts    float64   `json:"power_milliwatts"`
	SampledAt          time.Time `json:"sampled_at"`
	IntervalSeconds    float64   `json:"interval_seconds"`

	// PerProcessSupported separates "this hardware cannot attribute GPU
	// time to a process" from "nothing used the GPU this sample". The
	// powermetrics man page says per-process GPU time is available only
	// on certain hardware, and an empty Processes list would otherwise
	// read as an idle machine on a machine that simply cannot tell.
	PerProcessSupported bool      `json:"per_process_supported"`
	Processes           []ProcGPU `json:"processes,omitempty"`
}

// ProcGPU is one process's share of the GPU, in the unit powermetrics
// reports it: milliseconds of GPU time per second of wall clock, so
// 1000 means one whole GPU-second per second.
//
// Only processes that used the GPU during the sample are published. The
// overwhelming majority of a process table used none, and carrying those
// zeros would turn a few hundred bytes into a few hundred kilobytes
// rewritten every interval.
type ProcGPU struct {
	PID                   int     `json:"pid"`
	MillisecondsPerSecond float64 `json:"gpu_ms_per_second"`
}

// GPUPercent converts the reported unit to the same scale CPUPercent
// uses, where 100 means one fully occupied device.
func (p ProcGPU) GPUPercent() float64 {
	return p.MillisecondsPerSecond / 10
}

// mergeProcessGPU folds published per-process GPU time into an OS
// process table.
//
// It is a pure function over a table the caller already holds, so one
// read of the export file serves both the whole-machine figure and the
// per-process one. Sampling them separately would put a process's GPU
// share from one instant beside the machine's from another, which is the
// same reason a dashboard frame takes exactly one collection pass.
func mergeProcessGPU(procs []Proc, gpu *GPU) {
	if gpu == nil || len(gpu.Processes) == 0 {
		return
	}

	byPID := make(map[int]float64, len(gpu.Processes))
	for _, entry := range gpu.Processes {
		byPID[entry.PID] = entry.GPUPercent()
	}
	for index := range procs {
		procs[index].GPUPercent = byPID[procs[index].PID]
	}
}

// ReadGPU loads a published reading. A missing file, an unreadable one
// and a stale one are all ordinary states on a machine with no agent
// installed, so callers building a snapshot should discard the error
// rather than record it as a collector failure.
func ReadGPU(path string) (*GPU, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var gpu GPU
	if err := json.Unmarshal(raw, &gpu); err != nil {
		return nil, fmt.Errorf("sysmon: parse %s: %w", path, err)
	}
	if gpu.SampledAt.IsZero() {
		return nil, fmt.Errorf("sysmon: %s has no sampled_at", path)
	}

	if age := time.Since(gpu.SampledAt); age > gpu.staleAfter() {
		return nil, fmt.Errorf("%w: %s is %s old", ErrGPUStale, path, age.Round(time.Second))
	}
	return &gpu, nil
}

// staleAfter is the age at which this particular reading stops being
// worth showing.
func (g GPU) staleAfter() time.Duration {
	window := time.Duration(g.IntervalSeconds * float64(time.Second) * 3)
	if window < gpuStaleFloor {
		return gpuStaleFloor
	}
	return window
}
