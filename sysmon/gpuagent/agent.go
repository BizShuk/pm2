package gpuagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"github.com/bizshuk/pm2/sysmon"
)

// DefaultInterval is the sampling period.
//
// Thirty seconds, not the dashboard's own two: the `tasks` sampler walks
// the entire process table to attribute GPU time, so this is the most
// expensive thing pm2 runs, and it runs as root forever. A background
// agent that costs a process-table walk every two seconds is a worse
// trade than a GPU figure that is up to half a minute old.
const DefaultInterval = 30 * time.Second

// minInterval is the floor. powermetrics accepts far smaller periods,
// but below this the agent spends more time formatting text than the
// GPU spends drawing.
const minInterval = 200 * time.Millisecond

// stderrTailBytes bounds how much of powermetrics' stderr is kept for
// the error message. The useful part — "requires root privileges",
// "unrecognized sampler" — is always the first thing written, and an
// agent that runs for weeks must not accumulate warnings in memory.
const stderrTailBytes = 4 << 10

// waitDelay is how long Wait may block on the output pipe after the
// context is cancelled before the pipe is forcibly closed.
const waitDelay = 2 * time.Second

// Agent samples the GPU through powermetrics and republishes each
// reading to OutPath for unprivileged readers.
type Agent struct {
	OutPath  string
	Interval time.Duration
}

// Run drives one powermetrics process until ctx is cancelled.
//
// A single long-lived child is used rather than one invocation per
// sample: powermetrics costs a noticeable fraction of a second to start
// and its first sample is skewed by everything since boot, which is the
// same reason sysmon's darwin sampler reads iostat's second sample
// instead of top's first.
//
// Cancelling ctx is a normal stop, not an error — the caller wired
// SIGINT and SIGTERM to it.
func (a *Agent) Run(ctx context.Context) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("gpu agent: powermetrics exists only on macOS, not %s", runtime.GOOS)
	}
	if os.Geteuid() != 0 {
		return errors.New("gpu agent: powermetrics requires root — run under sudo or install the LaunchDaemon with `pm2 gpu install`")
	}

	path := a.OutPath
	if path == "" {
		path = sysmon.DefaultGPUExportPath
	}
	interval := a.Interval
	if interval < minInterval {
		interval = DefaultInterval
	}

	// One invocation covers both halves: `gpu_power` for the whole
	// machine and `tasks --show-process-gpu` for the per-process split.
	// Two powermetrics processes would each pay the sampling cost and
	// would report two different instants.
	milliseconds := strconv.FormatInt(interval.Milliseconds(), 10)
	cmd := exec.CommandContext(ctx, "powermetrics",
		"--samplers", "gpu_power,tasks",
		"--show-process-gpu",
		"-i", milliseconds)
	cmd.WaitDelay = waitDelay

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("gpu agent: stdout pipe: %w", err)
	}
	var stderr headBuffer
	stderr.limit = stderrTailBytes
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("gpu agent: start powermetrics: %w", err)
	}
	slog.Info("gpu agent started", "out", path, "interval", interval, "pid", cmd.Process.Pid)

	// A stopped agent must not leave its last reading behind: sysmon's
	// staleness check would eventually reject it, but until then the
	// dashboard would show a frozen number as if it were live.
	defer func() {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("gpu agent: could not remove export on exit", "err", err, "out", path, "source", "powermetrics")
		}
	}()

	published := 0
	parseErr := parseStream(stdout, func(gpu sysmon.GPU) {
		gpu.IntervalSeconds = interval.Seconds()
		if err := publish(path, gpu); err != nil {
			slog.Error("gpu agent: publish failed", "err", err, "out", path, "source", "powermetrics")
			return
		}
		published++
	})

	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		slog.Info("gpu agent stopped", "out", path, "published", published)
		return nil
	}
	if waitErr != nil {
		return fmt.Errorf("gpu agent: powermetrics exited: %w%s", waitErr, stderr.suffix())
	}
	if parseErr != nil {
		return fmt.Errorf("gpu agent: read powermetrics output: %w", parseErr)
	}
	return fmt.Errorf("gpu agent: powermetrics exited on its own after %d readings%s", published, stderr.suffix())
}

// headBuffer keeps the first limit bytes written to it and discards the
// rest.
type headBuffer struct {
	limit int
	data  []byte
}

func (h *headBuffer) Write(chunk []byte) (int, error) {
	if room := h.limit - len(h.data); room > 0 {
		if len(chunk) < room {
			room = len(chunk)
		}
		h.data = append(h.data, chunk[:room]...)
	}
	return len(chunk), nil
}

// suffix renders the captured output for appending to an error, or an
// empty string when the child said nothing.
func (h *headBuffer) suffix() string {
	if len(h.data) == 0 {
		return ""
	}
	return ": " + string(h.data)
}
