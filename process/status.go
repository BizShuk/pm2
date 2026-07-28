package process

// Status represents process lifecycle state.
type Status string

const (
	StatusOnline    Status = "online"
	StatusStopped   Status = "stopped"
	StatusStopping  Status = "stopping"
	StatusErrored   Status = "errored"
	StatusLaunching Status = "launching"
	// StatusPaused marks a process (typically a cron task) whose cron
	// schedule has been deliberately suspended via `pm2 task pause`. Unlike
	// StatusStopped — which a cron task also carries while idle between
	// fires — a paused task has NO scheduler entry and will not fire
	// until `pm2 task resume` re-registers it.
	StatusPaused Status = "paused"
)
