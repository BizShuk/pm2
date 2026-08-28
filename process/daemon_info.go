package process

import "time"

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

	// WebAddr is where the HTTP dashboard is listening, and WebError
	// why it is not. A failed web bind degrades the daemon rather than
	// failing it, so these two fields are how the refusal reaches a
	// human — `pm2 daemon status` prints them.
	WebAddr  string `json:"web_addr,omitempty"`
	WebError string `json:"web_error,omitempty"`
}
