package wizard

import "github.com/bizshuk/pm2/process"

// renderedApp is the user-authored ecosystem projection shared by the JS and
// JSON renderers. Runtime-only AppConfig fields are intentionally absent.
type renderedApp struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Script      string            `json:"script"`
	Args        []string          `json:"args,omitempty"`
	Instances   int               `json:"instances"`
	Env         map[string]string `json:"env,omitempty"`
	Cron        string            `json:"cron,omitempty"`
	CronRestart string            `json:"cron_restart,omitempty"`
	Watch       bool              `json:"watch,omitempty"`
	MaxRestarts int               `json:"max_restarts"`
	CWD         string            `json:"cwd,omitempty"`
	ConfigDir   string            `json:"config_dir,omitempty"`
	LogFile     string            `json:"log_file,omitempty"`
	OutFile     string            `json:"out_file,omitempty"`
	ErrorFile   string            `json:"error_file,omitempty"`
	Optional    bool              `json:"optional,omitempty"`
}

func appForRender(app process.AppConfig) renderedApp {
	app.Normalize("")

	configDir := app.ConfigDir
	if configDir == process.DefaultConfigDir(app.Name) {
		configDir = ""
	}

	logFile := app.LogFile
	if logFile == process.DefaultLogFile(app.ConfigDir) ||
		(app.OutFile != "" && app.OutFile == logFile) {
		logFile = ""
	}

	errorFile := app.ErrorFile
	if errorFile == process.DefaultErrorFile(app.ConfigDir) {
		errorFile = ""
	}

	return renderedApp{
		Name:        app.Name,
		Namespace:   app.Namespace,
		Script:      app.Script,
		Args:        app.Args,
		Instances:   app.Instances,
		Env:         app.Env,
		Cron:        app.Cron,
		CronRestart: app.CronRestart,
		Watch:       app.Watch,
		MaxRestarts: app.MaxRestarts,
		CWD:         app.CWD,
		ConfigDir:   configDir,
		LogFile:     logFile,
		OutFile:     app.OutFile,
		ErrorFile:   errorFile,
		Optional:    app.Optional,
	}
}
