package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/bizshuk/pm2/model"
)

// autoStartDaemon is the silent daemon start path used by CLI commands when
// their first socket request fails. It re-execs the current binary, waits for
// the daemon socket to accept RPC, and respects an explicit daemon-stop marker.
func autoStartDaemon() error {
	if hasDaemonStopMarker() {
		return fmt.Errorf(
			"daemon was stopped via 'pm2 daemon stop'; " +
				"auto-respawn is suppressed. Run 'pm2 daemon start' to re-enable",
		)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	command := exec.Command(exe, "daemon", "start", "--foreground")
	command.Stdout = nil
	command.Stderr = nil
	command.Stdin = nil

	if err := command.Start(); err != nil {
		return fmt.Errorf("spawn daemon: %w", err)
	}
	_ = command.Process.Release()

	sock := socketPath()
	for range 30 {
		time.Sleep(100 * time.Millisecond)
		resp, err := model.SendRequest(sock, model.Request{Command: model.CmdPing})
		if err == nil && resp.OK {
			return nil
		}
	}
	return fmt.Errorf("daemon did not start in time")
}

// hasDaemonStopMarker reports whether the explicit daemon-stop marker exists.
func hasDaemonStopMarker() bool {
	_, err := os.Stat(DaemonStopMarkerPath())
	return err == nil
}
