package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dop251/goja"

	"github.com/bizshuk/pm2/process"
)

// EcosystemConfig is the top-level config structure
type EcosystemConfig struct {
	Apps []process.AppConfig `json:"apps"`
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
		cfg.Apps[i].Normalize(configDir)
		cfg.Apps[i].ConfigFile = path
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
		cfg.Apps[i].Normalize(configDir)
		cfg.Apps[i].ConfigFile = path
	}
	return &cfg, nil
}
