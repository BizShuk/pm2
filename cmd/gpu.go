package cmd

import (
	"fmt"

	gpucmd "github.com/bizshuk/pm2/cmd/gpu"
	"github.com/spf13/cobra"
)

// GpuCmd is the `pm2 gpu` parent command. Bare `pm2 gpu` errors out so
// the caller always picks an explicit verb, matching `pm2 daemon`.
//
// The three verbs sit on opposite sides of a privilege boundary, and
// that is the whole point of the command group:
//
//   - `agent` and `install` are root work — sampling through
//     powermetrics and registering the LaunchDaemon that runs it.
//   - `status` is ordinary-user work — reading the file the agent
//     publishes, which is also all the daemon and the dashboard ever do.
//
// Subcommands live in cmd/gpu/:
//
//   - agent.go   — AgentCmd, the sampling loop launchd supervises
//   - status.go  — StatusCmd, the unprivileged reader
//   - install.go — InstallCmd + the LaunchDaemon definition
var GpuCmd = &cobra.Command{
	Use:   "gpu",
	Short: "Publish and read whole-machine GPU metrics (macOS)",
	Long: "macOS reports GPU residency and power through `powermetrics`,\n" +
		"which refuses to run as a normal user. Rather than make the pm2\n" +
		"daemon root — which would make every managed task root too —\n" +
		"a small privileged agent samples the GPU and publishes each\n" +
		"reading to a world-readable file that everything else only reads.\n\n" +
		"Subcommands: agent, status, install.",
	Args: cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		return fmt.Errorf("pm2 gpu requires a subcommand (agent | status | install)")
	},
}

func init() {
	GpuCmd.AddCommand(
		gpucmd.AgentCmd,
		gpucmd.StatusCmd,
		gpucmd.InstallCmd,
	)
}
