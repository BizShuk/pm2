package gpuagent

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/bizshuk/pm2/sysmon"
)

// sampleBoundary is the header powermetrics prints before every sample
// block. It is the only reliable delimiter in the output: the set of
// lines inside a block differs between Apple Silicon and Intel, and
// between OS releases, but the header has been stable for years.
const sampleBoundary = "*** Sampled system activity"

// scanBufferBytes bounds one line. powermetrics prints the per-frequency
// residency table on a single line, which is long on a many-state GPU
// and would abort bufio.Scanner's default 64 KiB budget on a future
// part with more states.
const scanBufferBytes = 1 << 20

// parseStream consumes powermetrics output and calls emit once per
// completed sample block.
//
// A block is only known to be complete when the *next* boundary arrives,
// so a published reading trails the machine by up to one interval. That
// is inherent to reading a line stream, not a lag worth engineering
// away — sysmon's staleness floor is set well above it.
func parseStream(reader io.Reader, emit func(sysmon.GPU)) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), scanBufferBytes)

	// The table lives outside the block because its column layout is a
	// property of the run, while its rows belong to one sample.
	var table taskTable
	var block sampleBlock
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, sampleBoundary) {
			if gpu, ok := block.result(table); ok {
				emit(gpu)
			}
			block, table.rows = sampleBlock{}, nil
			continue
		}
		// Machine-wide readings are `label: value`; a task row has no
		// colon at all, so an unrecognised line is always the table's.
		if !block.apply(line) {
			table.apply(line)
		}
	}
	if gpu, ok := block.result(table); ok {
		emit(gpu)
	}
	return scanner.Err()
}

// sampleBlock accumulates the GPU lines of one powermetrics sample.
type sampleBlock struct {
	utilization float64
	frequency   float64
	power       float64

	hasActive bool
	hasIdle   bool
	hasAny    bool
}

// apply folds one output line into the block and reports whether it was
// a machine-wide reading. Everything else — the preamble, the hardware
// detail, the task table — is left for the caller to route.
func (b *sampleBlock) apply(line string) bool {
	label, value, found := strings.Cut(line, ":")
	if !found {
		return false
	}

	switch strings.TrimSpace(label) {
	case "GPU HW active residency":
		// The authoritative utilisation figure on Apple Silicon. It is
		// followed by a per-frequency breakdown in parentheses, so only
		// the leading number is taken.
		if number, ok := firstFloat(value); ok {
			b.utilization, b.hasActive, b.hasAny = number, true, true
		}
	case "GPU idle residency":
		// Intel parts report only idle. Kept as a fallback so those
		// machines still get a utilisation number rather than a zero
		// that looks like a completely idle GPU.
		if number, ok := firstFloat(value); ok {
			b.hasIdle, b.hasAny = true, true
			if !b.hasActive {
				b.utilization = 100 - number
			}
		}
	case "GPU HW active frequency":
		if number, ok := firstFloat(value); ok {
			b.frequency, b.hasAny = number, true
		}
	case "GPU Power":
		if number, ok := firstFloat(value); ok {
			b.power, b.hasAny = number, true
		}
	default:
		return false
	}
	return true
}

// result returns the finished reading, or false when the block carried
// no GPU line at all — the preamble powermetrics prints before its first
// sample is one such block and must not be published as a reading of
// zero.
func (b *sampleBlock) result(table taskTable) (sysmon.GPU, bool) {
	if !b.hasAny {
		return sysmon.GPU{}, false
	}
	if b.hasActive || b.hasIdle {
		b.utilization = clampPercent(b.utilization)
	}
	return sysmon.GPU{
		Source:              "powermetrics",
		UtilizationPercent:  b.utilization,
		FrequencyMHz:        b.frequency,
		PowerMilliwatts:     b.power,
		SampledAt:           time.Now(),
		PerProcessSupported: table.supported,
		Processes:           table.rows,
	}, true
}

// firstFloat returns the first number in a value fragment, ignoring the
// unit or percent sign glued to it. It replaces one regexp per field
// with a single rule that copes with every shape powermetrics uses:
// "12.34%", " 444 MHz", "45 mW" and the bare ".19%" it prints for a
// residency below one percent.
func firstFloat(value string) (float64, bool) {
	start := -1
	for index, char := range value {
		numeric := (char >= '0' && char <= '9') || char == '.' || char == '-'
		switch {
		case numeric && start < 0:
			start = index
		case !numeric && start >= 0:
			return parseFloat(value[start:index])
		}
	}
	if start >= 0 {
		return parseFloat(value[start:])
	}
	return 0, false
}

func parseFloat(text string) (float64, bool) {
	number, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false
	}
	return number, true
}

func clampPercent(percent float64) float64 {
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}
