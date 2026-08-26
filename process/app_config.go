package process

import (
	"os"
	"path/filepath"
	"strings"
)

// AppConfig is the single source of truth for a managed process's static
// configuration. It is shared between the ecosystem config loader, the
// RPC start request, the runtime ProcessInfo, and the persisted
// dump.json snapshot — all four speak the same shape.
//
// JSON tags here are the wire / file contract; do not rename or reorder
// without an explicit migration plan.
type AppConfig struct {
	Namespace   string            `json:"namespace"`    // Default: "default"
	Name        string            `json:"name"`         // Default: script filename
	Script      string            `json:"script"`       // Required
	Args        []string          `json:"args"`         // Default: []
	Instances   int               `json:"instances"`    // Default: 1
	Env         map[string]string `json:"env"`          // Default: {}
	CronRestart string            `json:"cron_restart"` // Default: ""
	Cron        string            `json:"cron"`         // Default: ""
	Watch       bool              `json:"watch"`        // Default: false
	MaxRestarts int               `json:"max_restarts"` // Default: 15
	Version     string            `json:"version"`      // Default: "-"
	ConfigFile  string            `json:"config_file"`  // Default: "<cwd>/ecosystem.config.js"
	CWD         string            `json:"cwd"`          // Working directory when the process is spawned
	// BaseEnv is a snapshot of the CLI process environment (os.Environ()).
	// The CLI runs in the user's interactive shell, so this carries the full
	// PATH (and anything exported via .bashrc/.profile) through to the daemon,
	// which would otherwise spawn with its own minimal environment.
	BaseEnv []string `json:"base_env,omitempty"`
	// Paused indicates the process (typically a cron task) was deliberately
	// suspended via `pm2 task pause`. Persisted across save/resurrect so a daemon
	// restart does not silently re-enable a cron schedule the user paused.
	Paused bool `json:"paused,omitempty"`
	// Optional marks an app as inactive by default: `pm2 task start` registers
	// it paused unless the caller passes --all or names it via --with. The
	// zero value (false) means required, so an app that says nothing starts
	// immediately.
	//
	// This is an install policy field. The CLI translates it into Paused on
	// the start request; after registration, pause/resume controls runtime
	// state.
	Optional bool `json:"optional,omitempty"`
}

// Normalize fills in defaults for an AppConfig and resolves relative
// script paths against baseDir (typically the directory of the
// originating ecosystem.config.js).
//
// This is the only place defaults are applied; ecosystem loading,
// daemon resurrect, and any future entry points must call it.
func (a *AppConfig) Normalize(baseDir string) {
	if a.Instances <= 0 {
		a.Instances = DefaultInstances
	}
	if a.MaxRestarts <= 0 {
		a.MaxRestarts = DefaultMaxRestarts
	}
	if a.Namespace == "" {
		a.Namespace = DefaultNamespace
	}
	if a.Name == "" && a.Script != "" {
		base := filepath.Base(a.Script)
		a.Name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if a.ConfigFile == "" {
		cwd, err := os.Getwd()
		if err == nil {
			a.ConfigFile = filepath.Join(cwd, "ecosystem.config.js")
		} else {
			a.ConfigFile = "ecosystem.config.js"
		}
	}
	if a.CWD == "" && baseDir != "" {
		a.CWD = baseDir
	}
	if baseDir != "" {
		a.Script = ResolveScriptPath(baseDir, a.Script)
	}
}
