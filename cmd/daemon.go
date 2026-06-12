package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shuk/pm2/daemon"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "daemon",
		Short:  "Start the PM2 daemon (internal use)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			srv := daemon.NewServer(pm2Home)
			return srv.Listen(socketPath())
		},
	}
	return cmd
}

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
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>%s/daemon.log</string>
  <key>StandardErrorPath</key><string>%s/daemon-err.log</string>
</dict>
</plist>
`, label, exe, pm2Home, pm2Home)

	_ = os.MkdirAll(filepath.Dir(plistPath), 0755)
	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
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
ExecStart=%s daemon
Restart=always

[Install]
WantedBy=default.target
`, exe)

	unitPath := filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user", "pm2.service")
	_ = os.MkdirAll(filepath.Dir(unitPath), 0755)
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		return err
	}
	fmt.Printf("Created: %s\n", unitPath)
	fmt.Println("Enable with: systemctl --user enable pm2 && systemctl --user start pm2")
	return nil
}

// autoStartDaemon spawns the daemon as a detached background process
func autoStartDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exe, "daemon")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn daemon: %w", err)
	}
	_ = cmd.Process.Release()

	// Wait until daemon is ready (up to 3 seconds)
	sock := socketPath()
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		resp, err := daemon.SendRequest(sock, daemon.Request{Command: daemon.CmdPing})
		if err == nil && resp.OK {
			return nil
		}
	}
	return fmt.Errorf("daemon did not start in time")
}

// resolveTarget checks if a user argument is a name or numeric ID and returns the name
// (For simplicity, ID-based lookup is a TODO; name is used directly here)
func resolveTarget(s string) string {
	// Strip leading/trailing whitespace
	return strings.TrimSpace(s)
}
