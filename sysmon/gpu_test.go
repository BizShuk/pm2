package sysmon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeExport puts a reading on disk the way the agent does.
func writeExport(t *testing.T, gpu GPU) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pm2-gpu.json")
	encoded, err := json.Marshal(gpu)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestReadGPUReturnsFreshReading(t *testing.T) {
	path := writeExport(t, GPU{
		Source:             "powermetrics",
		UtilizationPercent: 42.5,
		FrequencyMHz:       1398,
		PowerMilliwatts:    8231,
		SampledAt:          time.Now(),
		IntervalSeconds:    2,
	})

	gpu, err := ReadGPU(path)
	if err != nil {
		t.Fatalf("ReadGPU: %v", err)
	}
	if gpu.UtilizationPercent != 42.5 || gpu.FrequencyMHz != 1398 || gpu.PowerMilliwatts != 8231 {
		t.Errorf("got %+v, want the published values round-tripped", *gpu)
	}
}

// A dead agent must not leave a number the dashboard keeps showing as
// if it were live.
func TestReadGPURejectsStaleReading(t *testing.T) {
	path := writeExport(t, GPU{
		Source:          "powermetrics",
		SampledAt:       time.Now().Add(-time.Minute),
		IntervalSeconds: 2,
	})

	if _, err := ReadGPU(path); !errors.Is(err, ErrGPUStale) {
		t.Fatalf("err = %v, want ErrGPUStale", err)
	}
}

// A deliberately slow agent must not be declared dead for keeping to
// its own schedule.
func TestReadGPUWidensStaleWindowForSlowAgent(t *testing.T) {
	path := writeExport(t, GPU{
		Source:          "powermetrics",
		SampledAt:       time.Now().Add(-30 * time.Second),
		IntervalSeconds: 60,
	})

	if _, err := ReadGPU(path); err != nil {
		t.Fatalf("ReadGPU: %v, want a 30s-old reading from a 60s agent to be accepted", err)
	}
}

// The absent case is the default state of every machine without an
// agent, and callers distinguish it from a corrupt file.
func TestReadGPUReportsMissingFileAsNotExist(t *testing.T) {
	_, err := ReadGPU(filepath.Join(t.TempDir(), "absent.json"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
}

// A reading with no timestamp cannot be aged, so it cannot be trusted.
func TestReadGPURejectsReadingWithoutTimestamp(t *testing.T) {
	path := writeExport(t, GPU{Source: "powermetrics", UtilizationPercent: 99})

	if _, err := ReadGPU(path); err == nil {
		t.Fatal("ReadGPU accepted a reading with no sampled_at")
	}
}

// Sample must never turn a missing agent into a collector error: an
// entry in Snapshot.Errors on every machine would train operators to
// ignore the field.
func TestSampleTreatsMissingGPUExportAsNormal(t *testing.T) {
	collector := &Collector{
		sampler: newFallbackSampler(),
		gpuPath: filepath.Join(t.TempDir(), "absent.json"),
	}

	system, err := collector.Sample()
	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("err = %v, want only the sampler's own error", err)
	}
	if system.GPU != nil {
		t.Errorf("GPU = %+v, want nil when nothing published a reading", system.GPU)
	}
}

func TestSampleAttachesPublishedGPUReading(t *testing.T) {
	path := writeExport(t, GPU{
		Source:             "powermetrics",
		UtilizationPercent: 73.5,
		SampledAt:          time.Now(),
		IntervalSeconds:    2,
	})
	collector := &Collector{sampler: newFallbackSampler(), gpuPath: path}

	system, _ := collector.Sample()
	if system.GPU == nil {
		t.Fatal("GPU = nil, want the published reading attached to the sample")
	}
	if system.GPU.UtilizationPercent != 73.5 {
		t.Errorf("utilization = %v, want 73.5", system.GPU.UtilizationPercent)
	}
}
