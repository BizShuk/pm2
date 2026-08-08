package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	"github.com/spf13/cobra"
)

// StartupCmd generates an OS-specific service definition for the daemon.
var StartupCmd = &cobra.Command{
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

func generateLaunchd(exe string) error {
	label := "com.shuk.pm2"
	plistPath := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", label+".plist")

	// Bootout / unload existing service if present
	uid := os.Getuid()
	target := fmt.Sprintf("gui/%d", uid)
	_ = exec.Command("launchctl", "bootout", target, plistPath).Run()
	_ = os.Remove(plistPath)

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
	</array>
	<key>EnvironmentVariables</key>
	<dict>
	<key>PATH</key>
	<string>%s</string>
	</dict>
	<key>RunAtLoad</key><true/>
	<key>StandardOutPath</key><string>%s/daemon.log</string>
	<key>StandardErrorPath</key><string>%s/daemon-err.log</string>
</dict>
</plist>
`, label, exe, os.Getenv("PATH"), cliruntime.PM2Home(), cliruntime.PM2Home())

	_ = os.MkdirAll(filepath.Dir(plistPath), 0o755)
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return err
	}
	fmt.Printf("Created: %s\n", plistPath)

	if err := exec.Command("launchctl", "bootstrap", target, plistPath).Run(); err != nil {
		if errLoad := exec.Command("launchctl", "load", plistPath).Run(); errLoad != nil {
			fmt.Printf("Enable with: launchctl bootstrap %s %s\n", target, plistPath)
			return nil
		}
	}
	fmt.Printf("Loaded service %s\n", label)
	return nil
}

func generateSystemd(exe string) error {
	unitPath := filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user", "pm2.service")

	// Stop/disable old systemd service if present
	_ = exec.Command("systemctl", "--user", "stop", "pm2.service").Run()
	_ = exec.Command("systemctl", "--user", "disable", "pm2.service").Run()
	_ = os.Remove(unitPath)

	unit := fmt.Sprintf(`[Unit]
Description=PM2 Daemon
After=network.target

[Service]
Type=simple
ExecStart=%s daemon start
Environment="PATH=%s"

[Install]
WantedBy=default.target
`, exe, os.Getenv("PATH"))

	_ = os.MkdirAll(filepath.Dir(unitPath), 0o755)
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return err
	}
	fmt.Printf("Created: %s\n", unitPath)

	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err == nil {
		_ = exec.Command("systemctl", "--user", "enable", "--now", "pm2.service").Run()
		fmt.Println("Loaded and enabled service pm2.service")
	} else {
		fmt.Println("Enable with: systemctl --user enable pm2 && systemctl --user start pm2")
	}
	return nil
}
