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
	LastCronAt     time.Time `json:"last_cron_at"`
	LastCronStatus string    `json:"last_cron_status"` // "ok" | "failed" | ""
	LogFile        string    `json:"log_file"`
	ErrorFile      string    `json:"error_file"`
	MaxRestarts    int       `json:"max_restarts"`
}

// DumpEntry is what gets persisted to dump.json for resurrect
type DumpEntry struct {
	Namespace   string            `json:"namespace"`
	Name        string            `json:"name"`
	Script      string            `json:"script"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	CronRestart string            `json:"cron_restart"`
	Instances   int               `json:"instances"`
	MaxRestarts int               `json:"max_restarts"`
	LogFile     string            `json:"log_file"`
	ErrorFile   string            `json:"error_file"`
}
