package process

import "time"

// ProcessInfo is the runtime view of a managed process. It embeds
// AppConfig for the static configuration and adds only the
// execution-time fields (PID, status, metrics, ...). JSON tags are
// kept on the runtime fields; the AppConfig fields are promoted and
// serialize to the top level just as before.
type ProcessInfo struct {
	AppConfig

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
