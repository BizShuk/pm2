package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

// newDaemonCmd returns the `pm2 daemon` parent command with `start`,
// `kill`, `stop`, and `status` subcommands. Bare `pm2 daemon` errors
// out so the caller always picks an explicit verb; the internal
// auto-spawn paths use `daemon start --foreground` directly.
//
// The four subcommands live in their own files:
//
//   - daemon_start.go  — newDaemonStartCmd + startDaemonAsBackground +
//     autoStartDaemon
//   - daemon_kill.go   — newDaemonKillCmd + runDaemonKill
//   - daemon_stop.go   — newDaemonStopCmd + runDaemonStop +
//     writeStopMarker / removeStopMarker / hasStopMarker
//   - daemon_status.go — newDaemonStatusCmd + runDaemonStatus
//
// This file keeps the parent itself plus `startup` (OS-specific
// boot-time daemon launch), which is its own concern.
func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the PM2 daemon",
		Long: "Start or stop the PM2 daemon. Subcommands: start, kill, stop, status.\n" +
			"`pm2 daemon start` spawns the daemon in the background (or in\n" +
			"the foreground with --foreground). `pm2 daemon kill` asks the\n" +
			"running daemon to shut down all managed processes and exit\n" +
			"(subsequent CLI commands may still auto-respawn it).\n" +
			"`pm2 daemon stop` does the same teardown as `kill` AND writes a\n" +
			"marker that suppresses the silent auto-respawn path used by other\n" +
			"CLI commands. Run `pm2 daemon start` to clear the marker.\n" +
			"`pm2 daemon status` reports the daemon's PID, version, and\n" +
			"runtime counters (read-only; works whether or not the daemon\n" +
			"is currently running).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("pm2 daemon requires a subcommand (start | kill | stop | status)")
		},
	}
	cmd.AddCommand(newDaemonStartCmd())
	cmd.AddCommand(newDaemonKillCmd())
	cmd.AddCommand(newDaemonStopCmd())
	cmd.AddCommand(newDaemonStatusCmd())
	return cmd
}

// newStartupCmd generates an OS-specific init script so the daemon
// launches on login/boot. The generated unit always re-execs the
// current binary with `daemon start --foreground` — same single
// source of truth as the user-facing start path.
func newStartupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "startup",
		Short: "Generate startup script for this OS",
		RunE: func(cmd *cobra.Command, args []string) error {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			switch runtime.GOOS {
			case "darwin":
				return generateLaunchd(exe)
			case "linux":
				return generateSystemd(exe)
			default:
				return fmt.Errorf("startup not supported on %s", runtime.GOOS)
			}
		},
	}
}

func generateLaunchd(exe string) error {
	label := "com.shuk.pm2"
	plistPath := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", label+".plist")

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key>
	<array>
	<string>%s</string>
	<string>daemon</string>
	<string>start</string>
	<string>--foreground</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
	<key>PATH</key>
	<string>%s</string>
	</dict>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>StandardOutPath</key><string>%s/daemon.log</string>
	<key>StandardErrorPath</key><string>%s/daemon-err.log</string>
</dict>
</plist>
`, label, exe, os.Getenv("PATH"), pm2Home, pm2Home)

	_ = os.MkdirAll(filepath.Dir(plistPath), 0o755)
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return err
	}
	fmt.Printf("Created: %s\n", plistPath)
	fmt.Printf("Enable with: launchctl load %s\n", plistPath)
	return nil
}

func generateSystemd(exe string) error {
	unit := fmt.Sprintf(`[Unit]
Description=PM2 Daemon
After=network.target

[Service]
Type=simple
ExecStart=%s daemon start --foreground
Environment="PATH=%s"
Restart=always

[Install]
WantedBy=default.target
`, exe, os.Getenv("PATH"))

	unitPath := filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user", "pm2.service")
	_ = os.MkdirAll(filepath.Dir(unitPath), 0o755)
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return err
	}
	fmt.Printf("Created: %s\n", unitPath)
	fmt.Println("Enable with: systemctl --user enable pm2 && systemctl --user start pm2")
	return nil
}