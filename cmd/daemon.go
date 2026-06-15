package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/shuk/pm2/daemon"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	var foreground bool
	cmd := &cobra.Command{
		Use:    "daemon",
		Short:  "Start the PM2 daemon (internal use)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if foreground {
				srv := daemon.NewServer(pm2Home)
				return srv.Listen(socketPath())
			}
			return startDaemonAsBackground()
		},
	}
	cmd.Flags().BoolVarP(&foreground, "foreground", "f", false, "Run the daemon in the foreground")
	return cmd
}

func startDaemonAsBackground() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	logDir := pm2Home
	_ = os.MkdirAll(logDir, 0o755)
	logFile := filepath.Join(logDir, "daemon.log")
	errFile := filepath.Join(logDir, "daemon-err.log")

	outF, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer outF.Close()

	errF, err := os.OpenFile(errFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer errF.Close()

	cmd := exec.Command(exe, "daemon", "--foreground")
	cmd.Stdout = outF
	cmd.Stderr = errF
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn daemon background: %w", err)
	}
	_ = cmd.Process.Release()

	fmt.Println("PM2 daemon started in the background.")
	return nil
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
ExecStart=%s daemon --foreground
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

// autoStartDaemon spawns the daemon as a detached background process
func autoStartDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exe, "daemon", "--foreground")
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
