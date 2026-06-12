package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dop251/goja"
)

// AppConfig mirrors PM2's app entry in ecosystem.config.js
type AppConfig struct {
	Namespace   string            `json:"namespace"`
	Name        string            `json:"name"`
	Script      string            `json:"script"`
	Args        []string          `json:"args"`
	Instances   int               `json:"instances"`
	Env         map[string]string `json:"env"`
	CronRestart string            `json:"cron_restart"`
	Cron        string            `json:"cron"`
	Watch       bool              `json:"watch"`
	MaxRestarts int               `json:"max_restarts"`
	Version     string            `json:"version"`
	LogFile     string            `json:"log_file"`
	OutFile     string            `json:"out_file"`
	ErrorFile   string            `json:"error_file"`
	ConfigDir   string            `json:"config_dir"`
}

// EcosystemConfig is the top-level config structure
type EcosystemConfig struct {
	Apps []AppConfig `json:"apps"`
}

// Normalize fills in defaults for an AppConfig
func (a *AppConfig) Normalize() {
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
}

// Load parses an ecosystem config file (.js or .json)
func Load(path string) (*EcosystemConfig, error) {
	absPath, err := filepath.Abs(path)
	if err == nil {
		path = absPath
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		return loadJSON(path)
	case ".js":
		return loadJS(path)
	default:
		return nil, fmt.Errorf("unsupported config format: %s", ext)
	}
}

func loadJSON(path string) (*EcosystemConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg EcosystemConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse json config: %w", err)
	}
	configDir := filepath.Dir(path)
	for i := range cfg.Apps {
		cfg.Apps[i].Normalize()
		cfg.Apps[i].Script = resolveScriptPath(configDir, cfg.Apps[i].Script)
	}
	return &cfg, nil
}

func loadJS(path string) (*EcosystemConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	vm := goja.New()

	// Provide a minimal module.exports shim
	moduleObj := vm.NewObject()
	_ = vm.Set("module", moduleObj)

	_, err = vm.RunString(string(data))
	if err != nil {
		return nil, fmt.Errorf("execute js config: %w", err)
	}

	exports := moduleObj.Get("exports")
	if exports == nil {
		return nil, fmt.Errorf("ecosystem.config.js must set module.exports")
	}

	jsonBytes, err := json.Marshal(exports.Export())
	if err != nil {
		return nil, fmt.Errorf("serialize js exports: %w", err)
	}

	var cfg EcosystemConfig
	if err := json.Unmarshal(jsonBytes, &cfg); err != nil {
		return nil, fmt.Errorf("parse js exports: %w", err)
	}
	configDir := filepath.Dir(path)
	for i := range cfg.Apps {
		cfg.Apps[i].Normalize()
		cfg.Apps[i].Script = resolveScriptPath(configDir, cfg.Apps[i].Script)
	}
	return &cfg, nil
}

// SingleApp wraps a bare app invocation (pm2 start script.js --name foo)
func SingleApp(script string, name string, args []string) AppConfig {
	cwd, err := os.Getwd()
	if err == nil {
		script = resolveScriptPath(cwd, script)
	}
	app := AppConfig{
		Script:    script,
		Name:      name,
		Args:      args,
		Instances: 1,
	}
	app.Normalize()
	return app
}

func resolveScriptPath(baseDir, script string) string {
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
	return script
}
