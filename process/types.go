package process

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Status represents process lifecycle state
type Status string

const (
	StatusOnline    Status = "online"
	StatusStopped   Status = "stopped"
	StatusStopping  Status = "stopping"
	StatusErrored   Status = "errored"
	StatusLaunching Status = "launching"
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
	LogFile     string            `json:"log_file"`     // Default: "~/.pm2/logs/<name>-out.log"
	OutFile     string            `json:"out_file"`     // Default: ""
	ErrorFile   string            `json:"error_file"`   // Default: "~/.pm2/logs/<name>-err.log"
	ConfigDir   string            `json:"config_dir"`   // Default: "~/.config/<name>/"
	ConfigFile  string            `json:"config_file"`  // Default: "<cwd>/ecosystem.config.js"
	CWD         string            `json:"cwd"`          // Working directory when the process is spawned
	// BaseEnv is a snapshot of the CLI process environment (os.Environ()).
	// The CLI runs in the user's interactive shell, so this carries the full
	// PATH (and anything exported via .bashrc/.profile) through to the daemon,
	// which would otherwise spawn with its own minimal environment.
	BaseEnv []string `json:"base_env,omitempty"`
}

// Normalize fills in defaults for an AppConfig and resolves relative
// script paths against baseDir (typically the directory of the
// originating ecosystem.config.js).
//
// This is the *only* place defaults are computed; ecosystem loading,
// daemon resurrect, and any future entry points must call it.
func (a *AppConfig) Normalize(baseDir string) {
	if a.Instances <= 0 {
		a.Instances = 1
	}
	if a.MaxRestarts <= 0 {
		a.MaxRestarts = 15
	}
	if a.Namespace == "" {
		a.Namespace = "default"
	}
	if a.Name == "" && a.Script != "" {
		base := filepath.Base(a.Script)
		a.Name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if a.ConfigDir == "" {
		if a.OutFile != "" {
			a.ConfigDir = filepath.Dir(a.OutFile)
		} else if a.LogFile != "" {
			a.ConfigDir = filepath.Dir(a.LogFile)
		} else if a.ErrorFile != "" {
			a.ConfigDir = filepath.Dir(a.ErrorFile)
		} else {
			a.ConfigDir = "~/.config/" + a.Name
		}
	}
	if a.ConfigDir != "" {
		if a.LogFile == "" && a.OutFile == "" {
			a.LogFile = filepath.Join(a.ConfigDir, "logs", "daemon.log")
		}
		if a.ErrorFile == "" {
			a.ErrorFile = filepath.Join(a.ConfigDir, "logs", "daemon.err")
		}
	}
	if a.LogFile == "" && a.OutFile != "" {
		a.LogFile = a.OutFile
	}
	if a.ConfigFile == "" {
		cwd, err := os.Getwd()
		if err == nil {
			a.ConfigFile = filepath.Join(cwd, "ecosystem.config.js")
		} else {
			a.ConfigFile = "ecosystem.config.js"
		}
	}
	if baseDir != "" {
		a.Script = ResolveScriptPath(baseDir, a.Script)
	}
}

// ProcessInfo is the runtime view of a managed process. It embeds
// AppConfig for the static configuration and adds only the
// execution-time fields (PID, status, metrics, ...). JSON tags are
// kept on the runtime fields; the AppConfig fields are promoted and
// serialize to the top level just as before.
type ProcessInfo struct {
	AppConfig

	// Runtime-only fields (everything else lives in AppConfig)
	ID             int       `json:"id"`
	PID            int       `json:"pid"`
	Status         Status    `json:"status"`
	Restarts       int       `json:"restarts"`
	StartedAt      time.Time `json:"started_at"`
	CPU            float64   `json:"cpu"`
	Memory         uint64    `json:"memory"`
	User           string    `json:"user"`
	LastCronAt     time.Time `json:"last_cron_at"`
	LastCronStatus string    `json:"last_cron_status"`
}

// DumpEntry is what gets persisted to dump.json for resurrect
//
// Deprecated: as of the unified-config refactor (see
// plans/architecture-unified-config.md and plans/gentle-finding-gizmo.md),
// dump.json is now serialized directly as []AppConfig. DumpEntry is
// retained briefly for any out-of-tree readers but the daemon no
// longer writes it.
type DumpEntry struct {
	Namespace   string            `json:"namespace"`
	Name        string            `json:"name"`
	Script      string            `json:"script"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	CronRestart string            `json:"cron_restart"`
	Cron        string            `json:"cron"`
	Instances   int               `json:"instances"`
	MaxRestarts int               `json:"max_restarts"`
	LogFile     string            `json:"log_file"`
	OutFile     string            `json:"out_file"`
	ErrorFile   string            `json:"error_file"`
	ConfigDir   string            `json:"config_dir"`
	Watch       bool              `json:"watch"`
	Version     string            `json:"version"`
	ConfigFile  string            `json:"config_file"`
	CWD         string            `json:"cwd"`
	BaseEnv     []string          `json:"base_env,omitempty"`
}

// ResolveScriptPath resolves a possibly-relative script path against
// baseDir. Absolute paths and bare commands resolvable via $PATH pass
// through unchanged. Lives in the process package so the AppConfig
// normalizer can call it without creating an import cycle.
func ResolveScriptPath(baseDir, script string) string {
	if script == "" || filepath.IsAbs(script) {
		return script
	}
	if filepath.Base(script) != script || strings.Contains(script, "/") || strings.Contains(script, string(filepath.Separator)) {
		return filepath.Join(baseDir, script)
	}
	targetPath := filepath.Join(baseDir, script)
	if _, err := os.Stat(targetPath); err == nil {
		return targetPath
	}
	if lookPath, err := exec.LookPath(script); err == nil {
		if absPath, err := filepath.Abs(lookPath); err == nil {
			return absPath
		}
		return lookPath
	}
	return script
}
