package cmd

import (
	"fmt"
	"os"

	"github.com/bizshuk/pm2/model"
)

// CLIClient centralises CLI↔daemon RPC: socket dial, the optional
// auto-respawn of the daemon on first-dial failure, and the
// `resp.OK` envelope check. Construct once per command invocation
// (cheap — no I/O at construction time) and reuse for the same
// socket path.
//
// CLIClient lives only in the `cmd` package. The TUI layer continues
// to call `model.SendRequest` directly to avoid a `tui/ -> cmd/`
// reverse dependency; cross-cutting concerns (test stubs, retry
// policies) belong in a higher layer when they're needed.
type CLIClient struct {
	socketPath string
}

// NewCLIClient returns a client bound to socketPath. The daemon is
// not dialed at construction time.
func NewCLIClient(socketPath string) *CLIClient {
	return &CLIClient{socketPath: socketPath}
}

// Send dials the daemon, writes req, reads resp, and closes the
// connection. If the first dial fails AND autoStartDaemon succeeds,
// Send retries exactly once with the same req. Caller still owns
// the resp.OK check (use SendOK to fold it in).
//
// The `autoStartDaemon` path honours `pm2 daemon stop`'s stop
// marker: when present, the helper returns an error and Send
// surfaces it as "cannot start daemon: ..." rather than silently
// respawning. This preserves the existing UX contract documented at
// `cmd/daemon_start.go:117-122`.
func (c *CLIClient) Send(req model.Request) (*model.Response, error) {
	resp, err := model.SendRequest(c.socketPath, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "daemon not running, starting it...")
		if startErr := autoStartDaemon(); startErr != nil {
			return nil, fmt.Errorf("cannot start daemon: %w", startErr)
		}
		resp, err = model.SendRequest(c.socketPath, req)
		if err != nil {
			return nil, err
		}
	}
	return resp, nil
}

// SendOK = Send + the resp.OK envelope check. On !OK it returns
// `fmt.Errorf("daemon: %s", resp.Error)`. Nil response is treated
// as a server-side error rather than a silent success.
func (c *CLIClient) SendOK(req model.Request) error {
	resp, err := c.Send(req)
	if err != nil {
		return err
	}
	if resp == nil || !resp.OK {
		return fmt.Errorf("daemon: %s", errorFrom(resp))
	}
	return nil
}

// errorFrom is a tiny helper to keep SendOK readable when resp is
// nil. Extracted so the nil branch is not buried in the main flow.
func errorFrom(resp *model.Response) string {
	if resp == nil {
		return "no response from daemon"
	}
	return resp.Error
}
