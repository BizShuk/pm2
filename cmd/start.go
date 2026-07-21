package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bizshuk/pm2/config"
	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
	"github.com/spf13/cobra"
)

// StartCmd starts processes from a local ecosystem file or remote repository.
var StartCmd = &cobra.Command{
	Use:   "start [ecosystem.config.js|ecosystem.config.json|owner/repo|https://github.com/...]",
	Short: "Start processes from an ecosystem file or a remote GitHub repo",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := "ecosystem.config.js"
		if len(args) > 0 {
			target = args[0]
		}

		// If target looks like a remote GitHub reference,
		// clone/pull into ~/.pm2/repos/ and resolve to the
		// ecosystem config inside.
		if config.IsRemoteRef(target) {
			cacheDir := filepath.Join(pm2Home, "repos")
			resolved, err := config.ResolveRemote(target, cacheDir)
			if err != nil {
				return fmt.Errorf("resolve remote %q: %w", target, err)
			}
			fmt.Fprintf(os.Stderr, "pm2: resolved remote %q -> %s\n", target, resolved)
			target = resolved
		}

		cfg, err := config.Load(target)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		for _, app := range cfg.Apps {
			req := model.Request{
				Command: model.CmdStart,
				App: &model.AppStartReq{
					AppConfig: app,
				},
			}
			// CLI environment snapshot travels in the embedded AppConfig.
			req.App.AppConfig.BaseEnv = os.Environ()

			client := NewCLIClient(socketPath())
			if err := client.SendOK(req); err != nil {
				return err
			}
			// Re-fetch the post-start response so we can render the
			// status block — the resp captured inside SendOK was
			// discarded by the !OK fold. We re-issue the same StartApp
			// request: the daemon is idempotent on same-name+same-script
			// (stop-and-replace), and the request embeds the original
			// AppConfig so a fresh send yields a fresh ProcessInfo
			// snapshot in the response payload.
			resp, err := client.Send(req)
			if err != nil {
				return err
			}
			if !resp.OK {
				return fmt.Errorf("daemon error: %s", resp.Error)
			}

			var infos []process.ProcessInfo
			if err := json.Unmarshal(resp.Payload, &infos); err == nil {
				for _, info := range infos {
					if info.PID <= 0 {
						fmt.Printf("[%d] %s scheduled\n", info.ID, info.Name)
					} else {
						fmt.Printf("[%d] %s started (pid=%d)\n", info.ID, info.Name, info.PID)
					}
					if info.Cron != "" {
						fmt.Printf("     cron:         %s\n", info.Cron)
					}
					if info.CronRestart != "" {
						fmt.Printf("     cron_restart: %s\n", info.CronRestart)
					}
				}
			}
		}
		return nil
	},
}
