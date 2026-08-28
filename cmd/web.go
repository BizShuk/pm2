package cmd

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"runtime"

	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
	"github.com/spf13/cobra"
)

// WebCmd prints the dashboard's URL and opens it.
//
// It is a client convenience, not a second server: the dashboard is
// served by the daemon itself. No short alias — `pm2 gpu` set the
// precedent that the alias table is the product's, not a pattern to
// extend by default.
var WebCmd = &cobra.Command{
	Use:   "web",
	Short: "Open the pm2 web dashboard",
	Args:  cobra.NoArgs,
	RunE:  runWeb,
}

func init() {
	WebCmd.Flags().Bool("no-open", false, "Print the URL instead of opening a browser")
}

func runWeb(cmd *cobra.Command, _ []string) error {
	noOpen, err := cmd.Flags().GetBool("no-open")
	if err != nil {
		return fmt.Errorf("read --no-open: %w", err)
	}

	// model.SendRequest, not the auto-starting client. An observer
	// asking "where is the dashboard" must not start a daemon in order
	// to answer — the same rule taskmanager and the emitter follow.
	resp, err := model.SendRequest(cliruntime.SocketPath(), model.Request{Command: model.CmdStatus})
	if err != nil || !resp.OK {
		return fmt.Errorf("daemon is not running; start it with: pm2 daemon start")
	}

	var info process.DaemonInfo
	if err := json.Unmarshal(resp.Payload, &info); err != nil {
		return fmt.Errorf("decode daemon status: %w", err)
	}
	if info.WebAddr == "" {
		if info.WebError != "" {
			return fmt.Errorf("web dashboard unavailable: %s", info.WebError)
		}
		return fmt.Errorf("web dashboard is disabled; start the daemon with --web-port <n>")
	}

	url := browsableURL(info.WebAddr)
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, url)
	fmt.Fprintln(out, "This dashboard and its webhook accept requests without authentication.")

	if noOpen {
		return nil
	}
	if err := openBrowser(url); err != nil {
		fmt.Fprintf(out, "(could not open a browser: %v)\n", err)
	}
	return nil
}

// browsableURL rewrites a wildcard bind into something a browser can
// actually resolve. 0.0.0.0 is an address to listen on, not one to visit.
func browsableURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return fmt.Errorf("no browser opener for %s", runtime.GOOS)
	}
	return cmd.Start()
}
