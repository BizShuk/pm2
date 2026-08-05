package task

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	cliruntime "github.com/bizshuk/pm2/cmd/runtime"
	"github.com/bizshuk/pm2/config"
	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
	"github.com/spf13/cobra"
)

// StartCmd runs tasks from a local ecosystem file or remote repository.
var StartCmd = &cobra.Command{
	Use:   "start [ecosystem.config.js|ecosystem.config.json|owner/repo|https://github.com/...]",
	Short: "Start tasks from an ecosystem file or remote GitHub repo (short alias: pm2 apply)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTasks,
	Example: `  pm2 task start ecosystem.config.js
  pm2 apply                     # short alias
  pm2 apply --single            # choose one app`,
}

func init() {
	bindStartFlags(StartCmd)
}

func bindStartFlags(command *cobra.Command) {
	command.Flags().Bool("all", false, "Run optional apps instead of registering them paused")
	command.Flags().StringSlice("with", nil, "Run these optional apps instead of registering them paused (repeatable or comma-separated)")
	command.Flags().Bool("single", false, "Choose and apply exactly one app from the ecosystem file")
	command.Flags().Bool("delete", false, "Delete every task declared by the ecosystem file instead of starting it")
}

// loadEcosystem resolves the CLI target (default ./ecosystem.config.js,
// optionally a remote GitHub reference) and parses it.
func loadEcosystem(args []string) (*config.EcosystemConfig, error) {
	target := "ecosystem.config.js"
	if len(args) > 0 {
		target = args[0]
	}

	// If target looks like a remote GitHub reference,
	// clone/pull into ~/.pm2/repos/ and resolve to the
	// ecosystem config inside.
	if config.IsRemoteRef(target) {
		cacheDir := filepath.Join(cliruntime.PM2Home(), "repos")
		resolved, err := config.ResolveRemote(target, cacheDir)
		if err != nil {
			return nil, fmt.Errorf("resolve remote %q: %w", target, err)
		}
		fmt.Fprintf(os.Stderr, "pm2: resolved remote %q -> %s\n", target, resolved)
		target = resolved
	}

	cfg, err := config.Load(target)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

func runTasks(cmd *cobra.Command, args []string) error {
	runDeleteAll, err := cmd.Flags().GetBool("delete")
	if err != nil {
		return fmt.Errorf("read --delete: %w", err)
	}

	cfg, err := loadEcosystem(args)
	if err != nil {
		return err
	}

	runAll, err := cmd.Flags().GetBool("all")
	if err != nil {
		return fmt.Errorf("read --all: %w", err)
	}
	runWith, err := cmd.Flags().GetStringSlice("with")
	if err != nil {
		return fmt.Errorf("read --with: %w", err)
	}
	runSingle, err := cmd.Flags().GetBool("single")
	if err != nil {
		return fmt.Errorf("read --single: %w", err)
	}
	if err := validateDeleteFlags(runDeleteAll, runSingle, runAll, runWith); err != nil {
		return err
	}
	if runDeleteAll {
		return deleteEcosystemApps(cfg.Apps, cmd.OutOrStdout())
	}
	if err := validateSelectionFlags(runSingle, runAll, runWith); err != nil {
		return err
	}
	var selected, paused []process.AppConfig
	if runSingle {
		app, chooseErr := chooseSingleApp(cfg.Apps, cmd.InOrStdin(), cmd.OutOrStdout())
		if chooseErr != nil {
			return chooseErr
		}
		selected = []process.AppConfig{app}
	} else {
		selected, paused, err = selectApps(cfg.Apps, runAll, runWith)
		if err != nil {
			return err
		}
	}
	for _, app := range paused {
		processKey := app.Name
		if app.Namespace != "" {
			processKey = app.Namespace + ":" + app.Name
		}
		fmt.Fprintf(os.Stderr, "pm2: optional app %q will be registered paused — resume it with: pm2 task resume %s\n",
			app.Name, processKey)
	}

	for _, app := range selected {
		req := model.Request{
			Command: model.CmdStart,
			App: &model.AppStartReq{
				AppConfig: app,
			},
		}
		// CLI environment snapshot travels in the embedded AppConfig.
		req.App.AppConfig.BaseEnv = os.Environ()

		client := cliruntime.NewCLIClient(cliruntime.SocketPath())
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
				if info.Status == process.StatusPaused {
					fmt.Printf("[%d] %s registered (paused)\n", info.ID, info.Name)
				} else if info.PID <= 0 {
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
}

// validateDeleteFlags keeps --delete a pure teardown verb: the selection
// flags only describe which apps to *start*, so combining them would ask
// the command to do two opposite things at once.
func validateDeleteFlags(deleteAll, single, all bool, with []string) error {
	if deleteAll && (single || all || len(with) > 0) {
		return fmt.Errorf("--delete cannot be used with --all, --with, or --single")
	}
	return nil
}

func validateSelectionFlags(single, all bool, with []string) error {
	if single && (all || len(with) > 0) {
		return fmt.Errorf("--single cannot be used with --all or --with")
	}
	return nil
}
