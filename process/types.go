package process

import "time"

// Status represents process lifecycle state
type Status string

const (
	StatusOnline   Status = "online"
	StatusStopped  Status = "stopped"
	StatusStopping Status = "stopping"
	StatusErrored  Status = "errored"
	StatusLaunching Status = "launching"
)

// ProcessInfo is the runtime view of a managed process
type ProcessInfo struct {
	ID          int       `json:"id"`
	Namespace   string    `json:"namespace"`
	Name        string    `json:"name"`
	PID         int       `json:"pid"`
	Status      Status    `json:"status"`
	Restarts    int       `json:"restarts"`
	StartedAt   time.Time `json:"started_at"`
	CPU         float64   `json:"cpu"`
	Memory      uint64    `json:"memory"`
	Script      string    `json:"script"`
	Args        []string  `json:"args"`
	Env         map[string]string `json:"env"`
	CronRestart    string    `json:"cron_restart"`
	Cron           string    `json:"cron"`
	LastCronAt     time.Time `json:"last_cron_at"`
	LastCronStatus string    `json:"last_cron_status"` // "ok" | "failed" | ""
	LogFile        string    `json:"log_file"`
	ErrorFile      string    `json:"error_file"`
	Version     string    `json:"version"`
	User        string    `json:"user"`
	Watch       bool      `json:"watch"`
	MaxRestarts    int       `json:"max_restarts"`
	ConfigDir      string    `json:"config_dir"`
	ConfigFile     string    `json:"config_file"`
	CWD            string    `json:"cwd"`
}

// DumpEntry is what gets persisted to dump.json for resurrect
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
}
