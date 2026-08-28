package web

import (
	"time"

	"github.com/bizshuk/pm2/process"
)

// taskView is the secret-stripping boundary.
//
// process.ProcessInfo embeds process.AppConfig, which carries Env and
// BaseEnv — the latter a snapshot of the user's interactive shell
// environment, taken by the CLI at apply time and shipped to the daemon
// so a task inherits the right PATH. Marshalling a ProcessInfo on this
// port would publish every exported token in the operator's shell
// profile to anyone who can reach it.
//
// Fields are listed one by one, deliberately: a struct built by
// subtraction goes wrong the next time ProcessInfo grows a field.
// TestTaskViewOmitsEnv pins the two that must never appear.
type taskView struct {
	ID             int       `json:"id"`
	Namespace      string    `json:"namespace"`
	Name           string    `json:"name"`
	Script         string    `json:"script"`
	Args           []string  `json:"args,omitempty"`
	CWD            string    `json:"cwd,omitempty"`
	Status         string    `json:"status"`
	PID            int       `json:"pid"`
	CPU            float64   `json:"cpu"`
	Memory         uint64    `json:"memory"`
	Restarts       int       `json:"restarts"`
	MaxRestarts    int       `json:"max_restarts"`
	StartedAt      time.Time `json:"started_at,omitzero"`
	Version        string    `json:"version,omitempty"`
	Watch          bool      `json:"watch"`
	Paused         bool      `json:"paused"`
	Optional       bool      `json:"optional"`
	Cron           string    `json:"cron,omitempty"`
	CronRestart    string    `json:"cron_restart,omitempty"`
	LastCronAt     time.Time `json:"last_cron_at,omitzero"`
	LastCronStatus string    `json:"last_cron_status,omitempty"`
	LogFile        string    `json:"log_file,omitempty"`
	ErrorFile      string    `json:"error_file,omitempty"`
}

func newTaskView(info process.ProcessInfo) taskView {
	return taskView{
		ID:             info.ID,
		Namespace:      info.Namespace,
		Name:           info.Name,
		Script:         info.Script,
		Args:           info.Args,
		CWD:            info.CWD,
		Status:         string(info.Status),
		PID:            info.PID,
		CPU:            info.CPU,
		Memory:         info.Memory,
		Restarts:       info.Restarts,
		MaxRestarts:    info.MaxRestarts,
		StartedAt:      info.StartedAt,
		Version:        info.Version,
		Watch:          info.Watch,
		Paused:         info.Paused,
		Optional:       info.Optional,
		Cron:           info.Cron,
		CronRestart:    info.CronRestart,
		LastCronAt:     info.LastCronAt,
		LastCronStatus: info.LastCronStatus,
		LogFile:        info.LogFile,
		ErrorFile:      info.ErrorFile,
	}
}

func newTaskViews(infos []process.ProcessInfo) []taskView {
	out := make([]taskView, 0, len(infos))
	for _, info := range infos {
		out = append(out, newTaskView(info))
	}
	return out
}

// daemonView is the same discipline applied to DaemonInfo.
type daemonView struct {
	PID          int       `json:"pid"`
	StartedAt    time.Time `json:"started_at,omitzero"`
	Version      string    `json:"version"`
	HomeDir      string    `json:"home_dir"`
	ProcessCount int       `json:"process_count"`
	WebAddr      string    `json:"web_addr,omitempty"`
	WebError     string    `json:"web_error,omitempty"`
}

func newDaemonView(info process.DaemonInfo) daemonView {
	return daemonView{
		PID:          info.PID,
		StartedAt:    info.StartedAt,
		Version:      info.Version,
		HomeDir:      info.HomeDir,
		ProcessCount: info.ProcessCount,
		WebAddr:      info.WebAddr,
		WebError:     info.WebError,
	}
}
