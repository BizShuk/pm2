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
	// StatusPaused marks a process (typically a cron task) whose cron
	// schedule has been deliberately suspended via `pm2 pause`. Unlike
	// StatusStopped — which a cron task also carries while idle between
	// fires — a paused task has NO scheduler entry and will not fire
	// until `pm2 resume` re-registers it.
	StatusPaused Status = "paused"
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
	// Paused indicates the process (typically a cron task) was deliberately
	// suspended via `pm2 pause`. Persisted across save/resurrect so a daemon
	// restart does not silently re-enable a cron schedule the user paused.
	Paused bool `json:"paused,omitempty"`
	// Optional marks an app as opt-in: `pm2 start` skips it unless the
	// caller passes --all or names it via --with. The zero value (false)
	// means required, so an app that says nothing is always installed.
	//
	// This is an *install policy* field, not a runtime one — it is read
	// once by the CLI when selecting which apps to send to the daemon and
	// has no effect on a process that is already registered.
	Optional bool `json:"optional,omitempty"`
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
			a.ConfigDir = "~/.config/" + NormalizeName(a.Name) + "/"
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

// DaemonInfo describes a running PM2 daemon. Returned by CmdStatus
// and rendered by `pm2 daemon status`. The struct shape is shared
// between wire (RPC payload) and future on-disk representations;
// today only the wire path is wired.
type DaemonInfo struct {
	PID          int       `json:"pid"`
	StartedAt    time.Time `json:"started_at"`
	Version      string    `json:"version"`
	HomeDir      string    `json:"home_dir"`
	ProcessCount int       `json:"process_count"`
}

// NormalizeName returns a filesystem-safe form of a process name for
// use as a path component: lowercased with spaces rewritten to hyphens.
// Used as the default ConfigDir segment when the user does not supply
// one (e.g. process "My App" → "my-app" → "~/.config/my-app/").
func NormalizeName(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", "-"))
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
