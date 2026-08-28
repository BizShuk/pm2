package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dop251/goja"

	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/workflow"
)

// EcosystemConfig is the top-level config structure
type EcosystemConfig struct {
	Apps      []process.AppConfig `json:"apps"`
	Workflows []workflow.Config   `json:"workflows"`
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
	if err := cfg.postProcess(path); err != nil {
		return nil, err
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
	if err := cfg.postProcess(path); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// postProcess applies defaults and validation to a freshly unmarshalled
// config. Both loaders call it, which is what keeps the .js and .json
// paths from drifting apart — they already converge on one
// json.Unmarshal, and this is the other half of that convergence.
//
// Workflow validation runs here, so a malformed `workflows:` block fails
// `pm2 apply` at parse time rather than at execution time. The cycle
// check over this one file is fast feedback only: the binding check is
// at daemon registration, where the whole registered set is visible.
func (c *EcosystemConfig) postProcess(path string) error {
	configDir := filepath.Dir(path)

	for i := range c.Apps {
		c.Apps[i].Normalize(configDir)
		c.Apps[i].ConfigFile = path
	}

	for i := range c.Workflows {
		c.Workflows[i].Normalize(configDir)
		c.Workflows[i].ConfigFile = path
	}
	if err := workflow.ValidateAll(c.Workflows); err != nil {
		return fmt.Errorf("invalid workflows in %s: %w", filepath.Base(path), err)
	}
	return nil
}
