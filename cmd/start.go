package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shuk/pm2/config"
	"github.com/shuk/pm2/daemon"
	"github.com/shuk/pm2/process"
	"github.com/spf13/cobra"
)

func newStartCmd() *cobra.Command {
	var (
		name        string
		instances   int
		cronRestart string
		envVars     []string
	)

	cmd := &cobra.Command{
		Use:   "start <script|ecosystem.config.js|ecosystem.config.json>",
		Short: "Start a process or ecosystem file",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			scriptArgs := args[1:] // args after the script path are passed through
			ext := strings.ToLower(filepath.Ext(target))

			var apps []config.AppConfig

			if ext == ".js" || ext == ".json" {
				cfg, err := config.Load(target)
				if err != nil {
					return fmt.Errorf("load config: %w", err)
				}
				apps = cfg.Apps
			} else {
				// Bare script path
				app := config.SingleApp(target, name, scriptArgs)
				if instances > 0 {
					app.Instances = instances
				}
				if cronRestart != "" {
					app.CronRestart = cronRestart
				}
				for _, e := range envVars {
					parts := strings.SplitN(e, "=", 2)
					if len(parts) == 2 {
						if app.Env == nil {
							app.Env = make(map[string]string)
						}
						app.Env[parts[0]] = parts[1]
					}
				}
				apps = []config.AppConfig{app}
			}

			for _, app := range apps {
				req := daemon.Request{
					Command: daemon.CmdStart,
					App: &daemon.AppStartReq{
						Name:        app.Name,
						Script:      app.Script,
						Args:        app.Args,
						Env:         app.Env,
						CronRestart: app.CronRestart,
						Instances:   app.Instances,
						MaxRestarts: app.MaxRestarts,
						LogFile:     app.LogFile,
						ErrorFile:   app.ErrorFile,
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
						fmt.Printf("[%d] %s started (pid=%d)\n", info.ID, info.Name, info.PID)
						if info.CronRestart != "" {
							fmt.Printf("     cron_restart: %s\n", info.CronRestart)
						}
					}
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&name, "name", "n", "", "process name")
	cmd.Flags().IntVarP(&instances, "instances", "i", 0, "number of instances")
	cmd.Flags().StringVar(&cronRestart, "cron-restart", "", "cron schedule for auto-restart")
	cmd.Flags().StringArrayVarP(&envVars, "env", "e", nil, "environment variables KEY=VAL")
	return cmd
}
