package dashboard

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/sysmon"
	"github.com/spf13/cobra"
)

// emitOptions are the user-facing flags for `pm2 dashboard emit`.
type emitOptions struct {
	interval time.Duration
	count    int
	output   string
	format   string
}

var emitOpts emitOptions

// EmitCmd streams complete system snapshots on a fixed period — the
// non-interactive half of the dashboard.
//
// The unit of output is one whole detection, not a delta: every record
// carries the machine's resources plus every managed task with its
// sub-processes and ports. A consumer that misses a record loses one
// sample rather than falling out of sync.
var EmitCmd = &cobra.Command{
	Use:   "emit",
	Short: "Emit a complete system snapshot on a fixed period",
	Long: "Write one full detection per interval: host CPU, memory,\n" +
		"network, disk I/O and filesystem usage, plus every managed task\n" +
		"with its sub-processes and listening ports.\n\n" +
		"`--format json` (the default) writes newline-delimited JSON, one\n" +
		"self-contained object per line, ready for `jq` or a log shipper.\n" +
		"`--format text` writes timestamped key=value lines matching the\n" +
		"managed-log convention, for piping into an existing log stream.\n\n" +
		"Runs until interrupted unless --count is given. The daemon is\n" +
		"never auto-started: a task list that cannot be read is reported\n" +
		"inside the snapshot and the machine readings still ship.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runEmit(cmd, &emitOpts)
	},
}

func init() {
	EmitCmd.Flags().DurationVar(&emitOpts.interval, "interval", sysmon.DefaultEmitInterval,
		"Period between snapshots (e.g. 10s, 1m)")
	EmitCmd.Flags().IntVar(&emitOpts.count, "count", 0,
		"Stop after this many snapshots (0 = run until interrupted)")
	EmitCmd.Flags().StringVar(&emitOpts.output, "out", "",
		"Append snapshots to this file instead of stdout")
	EmitCmd.Flags().StringVar(&emitOpts.format, "format", "json",
		"Output format: json (newline-delimited) or text")

	Cmd.AddCommand(EmitCmd)
}

func runEmit(cmd *cobra.Command, opts *emitOptions) error {
	encoder, err := snapshotEncoder(opts.format)
	if err != nil {
		return err
	}
	if opts.interval <= 0 {
		return fmt.Errorf("--interval must be positive, got %s", opts.interval)
	}

	out, closeOut, err := emitTarget(opts.output, cmd.OutOrStdout())
	if err != nil {
		return err
	}
	defer closeOut()

	socket := cliruntime.SocketPath()
	emitter := sysmon.NewEmitter(func() ([]process.ProcessInfo, error) {
		return model.ListProcesses(socket)
	})
	emitter.Encoder = encoder
	emitter.Interval = opts.interval
	emitter.Count = opts.count

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return emitter.Run(ctx, out)
}

func snapshotEncoder(format string) (sysmon.SnapshotEncoder, error) {
	switch format {
	case "json", "":
		return sysmon.EncodeJSONLine, nil
	case "text":
		return encodeText, nil
	default:
		return nil, fmt.Errorf("unknown --format %q (want json or text)", format)
	}
}

// emitTarget resolves where snapshots go. A file is opened for append so
// several runs, or a restart, accumulate into one stream rather than
// truncating the history.
func emitTarget(path string, fallback io.Writer) (io.Writer, func(), error) {
	if path == "" {
		return fallback, func() {}, nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("open snapshot output %q: %w", path, err)
	}
	return file, func() { _ = file.Close() }, nil
}
