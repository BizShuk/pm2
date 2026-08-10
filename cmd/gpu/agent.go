// Package gpu holds the subcommands of `pm2 gpu`.
package gpu

import (
	"os/signal"
	"syscall"
	"time"

	"github.com/bizshuk/pm2/sysmon"
	"github.com/bizshuk/pm2/sysmon/gpuagent"
	"github.com/spf13/cobra"
)

// AgentCmd is `pm2 gpu agent` — the privileged sampling loop.
//
// It always runs in the foreground. This is the same supervision
// contract `pm2 daemon start --foreground` carries: a process that
// detaches itself leaves launchd's direct child exiting 0 immediately,
// so the job is recorded as `state = not running` while the real worker
// reparents to PID 1 where nothing supervises it.
var AgentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Sample the GPU through powermetrics and publish each reading (root)",
	Long: "Runs powermetrics in the foreground and writes every sample to\n" +
		"--out as JSON, atomically, so unprivileged readers never see a\n" +
		"half-written file. Requires root; this is normally started by the\n" +
		"LaunchDaemon that `pm2 gpu install` registers, not by hand.\n\n" +
		"The export file is removed on exit so a stopped agent cannot\n" +
		"leave a frozen reading behind.",
	Args: cobra.NoArgs,
	RunE: runAgent,
}

var (
	agentOut      string
	agentInterval time.Duration
)

func init() {
	AgentCmd.Flags().StringVar(&agentOut, "out", sysmon.DefaultGPUExportPath,
		"path to publish readings to")
	AgentCmd.Flags().DurationVar(&agentInterval, "interval", gpuagent.DefaultInterval,
		"sampling period")
}

func runAgent(cmd *cobra.Command, _ []string) error {
	// SIGTERM is how launchd stops the job and SIGINT is how a human
	// stops a foreground run; both mean "publish nothing further and
	// clean up", which is exactly what cancelling the context does.
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	agent := &gpuagent.Agent{OutPath: agentOut, Interval: agentInterval}
	return agent.Run(ctx)
}
