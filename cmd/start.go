package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/bizshuk/pm2/config"
	"github.com/bizshuk/pm2/daemon"
	"github.com/bizshuk/pm2/process"
	"github.com/spf13/cobra"
)

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start [ecosystem.config.js|ecosystem.config.json]",
		Short: "Start processes from an ecosystem file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "ecosystem.config.js"
			if len(args) > 0 {
				target = args[0]
			}

			cfg, err := config.Load(target)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			for _, app := range cfg.Apps {
				req := daemon.Request{
					Command: daemon.CmdStart,
					App: &daemon.AppStartReq{
						Namespace:   app.Namespace,
						Name:        app.Name,
						Script:      app.Script,
						Args:        app.Args,
						Env:         app.Env,
						CronRestart: app.CronRestart,
						Cron:        app.Cron,
						Instances:   app.Instances,
						MaxRestarts: app.MaxRestarts,
						LogFile:     app.LogFile,
						OutFile:     app.OutFile,
						ErrorFile:   app.ErrorFile,
						ConfigDir:   app.ConfigDir,
						Watch:       app.Watch,
						Version:     app.Version,
						ConfigFile:  app.ConfigFile,
						CWD:         app.CWD,
						BaseEnv:     os.Environ(),
					},
				}

				resp, err := daemon.SendRequest(socketPath(), req)
				if err != nil {
					// Try to auto-start the daemon
					fmt.Fprintln(os.Stderr, "daemon not running, starting it...")
					if startErr := autoStartDaemon(); startErr != nil {
						return fmt.Errorf("cannot start daemon: %w", startErr)
					}
					resp, err = daemon.SendRequest(socketPath(), req)
					if err != nil {
						return err
					}
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
	return cmd
}
