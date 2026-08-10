package gpuagent

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bizshuk/pm2/sysmon"
)

// appleSiliconOutput is captured `powermetrics --samplers gpu_power -i 1000`
// output: a preamble, then two sample blocks.
const appleSiliconOutput = `Machine model: Mac14,6
OS version: 23F79
Boot time: Wed Jul  9 09:12:31 2025


*** Sampled system activity (Sat Aug  9 11:20:03 2026 -0700) (1002.35ms elapsed) ***


**** GPU usage ****

GPU HW active frequency: 444 MHz
GPU HW active residency:  12.34% (444 MHz:  12% 612 MHz: .19% 808 MHz:   0%)
GPU SW requested state: (P1 :   0% P2 : 100% P3 :   0%)
GPU SW state: (SW_P1 :   0% SW_P2 : 100%)
GPU idle residency:  87.66%
GPU Power: 45 mW


*** Sampled system activity (Sat Aug  9 11:20:04 2026 -0700) (1001.02ms elapsed) ***


**** GPU usage ****

GPU HW active frequency: 1398 MHz
GPU HW active residency:  99.90% (1398 MHz: 100%)
GPU idle residency:   0.10%
GPU Power: 8231 mW
`

func collect(t *testing.T, output string) []sysmon.GPU {
	t.Helper()
	var readings []sysmon.GPU
	if err := parseStream(strings.NewReader(output), func(gpu sysmon.GPU) {
		readings = append(readings, gpu)
	}); err != nil {
		t.Fatalf("parseStream: %v", err)
	}
	return readings
}

func TestParseStreamReadsEverySampleBlock(t *testing.T) {
	readings := collect(t, appleSiliconOutput)

	if len(readings) != 2 {
		t.Fatalf("got %d readings, want 2 — one per sample block", len(readings))
	}
	first, second := readings[0], readings[1]
	if first.UtilizationPercent != 12.34 || first.FrequencyMHz != 444 || first.PowerMilliwatts != 45 {
		t.Errorf("first = %+v, want 12.34%% / 444 MHz / 45 mW", first)
	}
	if second.UtilizationPercent != 99.90 || second.FrequencyMHz != 1398 || second.PowerMilliwatts != 8231 {
		t.Errorf("second = %+v, want 99.90%% / 1398 MHz / 8231 mW", second)
	}
	if first.Source != "powermetrics" {
		t.Errorf("source = %q, want powermetrics", first.Source)
	}
}

// The preamble powermetrics prints before its first sample carries no
// GPU line. Publishing it would show a fully idle GPU for one interval
// every time the agent restarted.
func TestParseStreamSkipsPreambleWithNoGPULines(t *testing.T) {
	readings := collect(t, "Machine model: Mac14,6\nOS version: 23F79\n")

	if len(readings) != 0 {
		t.Fatalf("got %d readings, want none from output with no GPU lines", len(readings))
	}
}

// Intel parts report idle residency and nothing else. Falling back to
// 100-idle keeps those machines on a real number instead of a zero that
// reads as a completely idle GPU.
func TestParseStreamDerivesUtilizationFromIdleAlone(t *testing.T) {
	readings := collect(t, `*** Sampled system activity ***
GPU idle residency:  90.00%
GPU Power: 120 mW
`)

	if len(readings) != 1 {
		t.Fatalf("got %d readings, want 1", len(readings))
	}
	if readings[0].UtilizationPercent != 10 {
		t.Errorf("utilization = %v, want 10 derived from 90%% idle", readings[0].UtilizationPercent)
	}
}

// A block that reports both must prefer the direct measurement, not the
// derived one.
func TestParseStreamPrefersActiveResidencyOverIdle(t *testing.T) {
	readings := collect(t, `*** Sampled system activity ***
GPU idle residency:  10.00%
GPU HW active residency:  88.00% (444 MHz: 88%)
`)

	if readings[0].UtilizationPercent != 88 {
		t.Errorf("utilization = %v, want the reported active residency of 88", readings[0].UtilizationPercent)
	}
}

func TestFirstFloatHandlesEveryShapePowermetricsPrints(t *testing.T) {
	cases := map[string]float64{
		"  12.34% (444 MHz: 12%)": 12.34,
		" 444 MHz":                444,
		" 8231 mW":                8231,
		"   .19%":                 0.19,
		"  0.00%":                 0,
	}
	for input, want := range cases {
		got, ok := firstFloat(input)
		if !ok || got != want {
			t.Errorf("firstFloat(%q) = %v, %v; want %v, true", input, got, ok, want)
		}
	}
	if _, ok := firstFloat(" (no number here)"); ok {
		t.Error("firstFloat found a number in a value with none")
	}
}

// The whole design rests on an unprivileged reader being able to open
// what a root writer produced, and on never seeing a partial file.
func TestPublishWritesAtomicallyAndWorldReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pm2-gpu.json")
	reading := sysmon.GPU{
		Source:             "powermetrics",
		UtilizationPercent: 42,
		SampledAt:          time.Now(),
		IntervalSeconds:    2,
	}

	if err := publish(path, reading); err != nil {
		t.Fatalf("publish: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != exportFileMode {
		t.Errorf("mode = %v, want %v so an unprivileged reader can open it", info.Mode().Perm(), os.FileMode(exportFileMode))
	}

	got, err := sysmon.ReadGPU(path)
	if err != nil {
		t.Fatalf("ReadGPU: %v", err)
	}
	if got.UtilizationPercent != 42 {
		t.Errorf("utilization = %v, want 42", got.UtilizationPercent)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d files, want only the published one — a temp file leaked", len(entries))
	}
}

func TestPublishReplacesThePreviousReading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pm2-gpu.json")
	for _, percent := range []float64{10, 20, 30} {
		if err := publish(path, sysmon.GPU{Source: "powermetrics", UtilizationPercent: percent, SampledAt: time.Now(), IntervalSeconds: 2}); err != nil {
			t.Fatalf("publish %v: %v", percent, err)
		}
	}

	got, err := sysmon.ReadGPU(path)
	if err != nil {
		t.Fatalf("ReadGPU: %v", err)
	}
	if got.UtilizationPercent != 30 {
		t.Errorf("utilization = %v, want the last published value 30", got.UtilizationPercent)
	}
}

// Root is the one precondition the agent cannot work around, and the
// message has to say what to do about it.
func TestRunRefusesWithoutRoot(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the platform check fires first off macOS")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root; the refusal path is unreachable")
	}
	agent := &Agent{OutPath: filepath.Join(t.TempDir(), "pm2-gpu.json")}

	err := agent.Run(t.Context())
	if err == nil {
		t.Fatal("Run succeeded without root")
	}
	if !strings.Contains(err.Error(), "pm2 gpu install") {
		t.Errorf("err = %q, want it to point at the install command", err)
	}
}

// tasksOutput is the shape `--samplers tasks --show-process-gpu` prints:
// a name column that contains spaces, numeric columns right-aligned
// under their headings, and an ALL_TASKS aggregate at the end.
const tasksOutput = `*** Sampled system activity (Sat Aug  9 11:20:03 2026 -0700) (30002.35ms elapsed) ***

**** GPU usage ****

GPU HW active frequency: 444 MHz
GPU HW active residency:  12.34% (444 MHz: 12%)
GPU Power: 45 mW

**** Running tasks ****

Name                             ID     CPU ms/s  User%  GPU ms/s
WindowServer                     343    123.45    50.00    456.70
Google Chrome Helper (GPU)       9182   45.10     10.00     78.90
loginwindow                      512    0.10      0.00       0.00
ALL_TASKS                        -2     999.99    99.99    535.60

*** Sampled system activity (Sat Aug  9 11:20:33 2026 -0700) (30001.02ms elapsed) ***
`

func TestParseStreamAttributesGPUTimePerProcess(t *testing.T) {
	readings := collect(t, tasksOutput)

	if len(readings) != 1 {
		t.Fatalf("got %d readings, want 1", len(readings))
	}
	gpu := readings[0]
	if !gpu.PerProcessSupported {
		t.Error("PerProcessSupported = false, want true when the table has a GPU column")
	}

	// Only processes that used the GPU are published, and the ALL_TASKS
	// aggregate is not a process.
	if len(gpu.Processes) != 2 {
		t.Fatalf("got %d processes, want 2: %+v", len(gpu.Processes), gpu.Processes)
	}
	want := map[int]float64{343: 456.70, 9182: 78.90}
	for _, entry := range gpu.Processes {
		if got, ok := want[entry.PID]; !ok || entry.MillisecondsPerSecond != got {
			t.Errorf("process %+v is not one of the expected rows %v", entry, want)
		}
	}

	// A name with spaces must not shift the numeric columns.
	for _, entry := range gpu.Processes {
		if entry.PID == 9182 && math.Abs(entry.GPUPercent()-7.89) > 0.001 {
			t.Errorf("GPUPercent = %v, want ms/s 78.90 scaled to 7.89", entry.GPUPercent())
		}
	}
	// The whole-machine reading in the same block still parses.
	if gpu.UtilizationPercent != 12.34 || gpu.PowerMilliwatts != 45 {
		t.Errorf("host reading = %+v, want the gpu_power block unaffected by the task table", gpu)
	}
}

// Hardware that cannot attribute GPU time prints the table without a GPU
// column. Reporting that as "no process used the GPU" would send an
// operator hunting for something that was never measurable.
func TestParseStreamReportsUnsupportedPerProcessGPU(t *testing.T) {
	readings := collect(t, `*** Sampled system activity ***

GPU HW active residency:  12.34%

**** Running tasks ****

Name                             ID     CPU ms/s  User%
WindowServer                     343    123.45    50.00

*** Sampled system activity ***
`)

	if len(readings) != 1 {
		t.Fatalf("got %d readings, want 1", len(readings))
	}
	if readings[0].PerProcessSupported {
		t.Error("PerProcessSupported = true, want false when the table has no GPU column")
	}
	if len(readings[0].Processes) != 0 {
		t.Errorf("Processes = %+v, want none", readings[0].Processes)
	}
}

// The header is printed once per run, not once per sample. Clearing what
// the table learned at every boundary would make every reading after the
// first look like unsupported hardware.
func TestParseStreamKeepsPerProcessSupportAcrossSamples(t *testing.T) {
	// The second block repeats the table without repeating the header,
	// exactly as powermetrics does, and carries its own gpu_power
	// section the way a real `gpu_power,tasks` run always does.
	readings := collect(t, tasksOutput+`
GPU HW active residency:  5.00%

**** Running tasks ****

WindowServer                     343    100.00    50.00    200.00

*** Sampled system activity ***
`)

	if len(readings) < 2 {
		t.Fatalf("got %d readings, want at least 2", len(readings))
	}
	for index, reading := range readings {
		if !reading.PerProcessSupported {
			t.Errorf("reading %d lost PerProcessSupported", index)
		}
	}
	// The header was printed once. The second sample's rows must still
	// be attributed, or per-process GPU would work for one sample and
	// then quietly stop.
	second := readings[1]
	if len(second.Processes) != 1 || second.Processes[0].PID != 343 {
		t.Errorf("second reading processes = %+v, want pid 343 parsed from a headerless table", second.Processes)
	}
}

// Rows belong to the sample that produced them; carrying them forward
// would make a process that used the GPU once appear to use it forever.
func TestParseStreamDoesNotCarryProcessRowsBetweenSamples(t *testing.T) {
	readings := collect(t, tasksOutput+`
GPU HW active residency:  5.00%

*** Sampled system activity ***
`)

	last := readings[len(readings)-1]
	if len(last.Processes) != 0 {
		t.Errorf("last reading carried %d stale process rows: %+v", len(last.Processes), last.Processes)
	}
}
