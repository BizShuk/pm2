package gpu

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/bizshuk/pm2/sysmon"
	"github.com/bizshuk/pm2/sysmon/gpuagent"
	"github.com/spf13/cobra"
)

// launchDaemonDir is the system-domain job directory. Only root may
// write here, which is the same permission the job itself needs — so a
// user who cannot install the job could not have run the agent anyway.
const launchDaemonDir = "/Library/LaunchDaemons"

// InstallCmd is `pm2 gpu install`.
//
// This file owns the install side (where the definition goes, which CLI
// loads it); the definition itself — and the supervision contract it
// encodes — lives in install_template.go. That split mirrors
// cmd/startup.go and cmd/startup_template.go, and for the same reason:
// the two service definitions pm2 ships are one contract written twice
// and have already drifted apart once.
var InstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Register the privileged GPU agent as a LaunchDaemon (run with sudo)",
	Long: "Writes " + launchDaemonDir + "/" + gpuAgentLabel + ".plist and\n" +
		"bootstraps it, so the GPU agent starts at boot and is restarted if\n" +
		"powermetrics dies. Requires root.\n\n" +
		"Remove it again with:\n" +
		"  sudo launchctl bootout system/" + gpuAgentLabel + "\n" +
		"  sudo rm " + launchDaemonDir + "/" + gpuAgentLabel + ".plist",
	Args: cobra.NoArgs,
	RunE: runInstall,
}

var (
	installOut      string
	installInterval time.Duration
)

func init() {
	InstallCmd.Flags().StringVar(&installOut, "out", sysmon.DefaultGPUExportPath,
		"path the agent publishes readings to")
	InstallCmd.Flags().DurationVar(&installInterval, "interval", gpuagent.DefaultInterval,
		"sampling period baked into the job definition")
}

func runInstall(cmd *cobra.Command, _ []string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("pm2 gpu install: powermetrics exists only on macOS, not %s", runtime.GOOS)
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("pm2 gpu install: writing %s requires root — re-run with sudo", launchDaemonDir)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	plistPath := filepath.Join(launchDaemonDir, gpuAgentLabel+".plist")

	// Bootout before rewriting: launchd caches the definition it loaded,
	// so replacing the file under a live job leaves the old argv running.
	_ = exec.Command("launchctl", "bootout", "system/"+gpuAgentLabel).Run()
	_ = os.Remove(plistPath)

	// Bake --interval into the job only when the caller actually asked
	// for one. Otherwise the agent follows its own default, so changing
	// that default is a rebuild rather than a rebuild plus a privileged
	// reinstall nobody remembers to run.
	seconds := 0
	if cmd.Flags().Changed("interval") {
		seconds = max(int(installInterval.Seconds()), 1)
	}
	plist := launchDaemonPlist(exe, installOut, seconds)
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Created: %s\n", plistPath)

	if err := exec.Command("launchctl", "bootstrap", "system", plistPath).Run(); err != nil {
		fmt.Fprintf(out, "Enable with: sudo launchctl bootstrap system %s\n", plistPath)
		return nil
	}
	fmt.Fprintf(out, "Loaded service %s\n", gpuAgentLabel)
	effective := gpuagent.DefaultInterval
	if seconds > 0 {
		effective = time.Duration(seconds) * time.Second
	}
	fmt.Fprintf(out, "Publishing to %s every %s — check with: pm2 gpu status\n", installOut, effective)
	fmt.Fprintf(out, "Remove with: sudo launchctl bootout system/%s && sudo rm %s\n", gpuAgentLabel, plistPath)
	return nil
}
