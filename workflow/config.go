package workflow

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bizshuk/pm2/process"
)

// DefaultCategory groups workflows that declare no category, mirroring
// process.DefaultNamespace for apps.
const DefaultCategory = "default"

// StageKind is which of the three mutually exclusive stage forms a
// stage uses.
type StageKind string

const (
	StageScript   StageKind = "script"
	StageTask     StageKind = "task"
	StageWorkflow StageKind = "workflow"
)

// Stage is one step of a workflow.
//
// Exactly one of Script, Task, or Workflow must be set. Args, Env, and
// CWD belong to Script alone: a task stage runs the registered task's
// own command and environment, and a workflow stage runs another
// workflow, so supplying them there would silently do nothing.
type Stage struct {
	Name     string            `json:"name"`
	Script   string            `json:"script,omitempty"`
	Args     []string          `json:"args,omitempty"`
	Task     string            `json:"task,omitempty"`
	Workflow string            `json:"workflow,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	CWD      string            `json:"cwd,omitempty"`
	Timeout  string            `json:"timeout,omitempty"`
}

// Kind reports which form this stage uses, or "" when it is not exactly
// one of the three.
func (s Stage) Kind() StageKind {
	switch {
	case s.Script != "" && s.Task == "" && s.Workflow == "":
		return StageScript
	case s.Task != "" && s.Script == "" && s.Workflow == "":
		return StageTask
	case s.Workflow != "" && s.Script == "" && s.Task == "":
		return StageWorkflow
	default:
		return ""
	}
}

// Ref is what a task or workflow stage points at; empty for a script stage.
func (s Stage) Ref() string {
	switch s.Kind() {
	case StageTask:
		return s.Task
	case StageWorkflow:
		return s.Workflow
	default:
		return ""
	}
}

// Config is one declared workflow, as written in the `workflows:` block
// of an ecosystem file.
//
// The JSON tags are the file and wire contract; do not rename them
// without a migration plan.
type Config struct {
	Category string            `json:"category"`
	Name     string            `json:"name"`
	Stages   []Stage           `json:"stages"`
	Cron     string            `json:"cron,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	CWD      string            `json:"cwd,omitempty"`
	Timeout  string            `json:"timeout,omitempty"`

	ConfigFile string `json:"config_file,omitempty"`

	// BaseEnv is the CLI's os.Environ() snapshot, stamped at apply time
	// so a stage script sees the user's PATH rather than the daemon's
	// minimal environment. Never written by hand in a config file.
	BaseEnv []string `json:"base_env,omitempty"`
}

// Key is the workflow's identity: "<category>:<name>".
func (c Config) Key() string { return c.Category + ":" + c.Name }

// ParseKey splits a reference. A bare name yields an empty category,
// which callers resolve against DefaultCategory or by unique-name match.
func ParseKey(ref string) (category, name string) {
	if i := strings.Index(ref, ":"); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return "", ref
}

// Normalize fills defaults and resolves relative script paths against
// baseDir, the directory of the ecosystem file. Only script stages have
// their path resolved — a task or workflow reference is a name, not a
// path, and rewriting it would turn "unit-tests" into a directory entry.
//
// It deliberately does not invent a name: an unnamed app derives one
// from its script filename, but a workflow has no single script to
// derive from, so an unnamed workflow is a validation error.
func (c *Config) Normalize(baseDir string) {
	if c.Category == "" {
		c.Category = DefaultCategory
	}
	if c.CWD == "" {
		c.CWD = baseDir
	}
	for i := range c.Stages {
		st := &c.Stages[i]
		if st.Name == "" {
			st.Name = fmt.Sprintf("stage-%d", i+1)
		}
		if st.CWD == "" {
			st.CWD = c.CWD
		}
		if st.Kind() == StageScript && baseDir != "" {
			st.Script = process.ResolveScriptPath(baseDir, st.Script)
		}
	}
}

// Validate reports the first structural problem with the workflow. Every
// message names the workflow and, where relevant, the stage.
func (c Config) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("workflow in %q: name is required", c.ConfigFile)
	}
	if len(c.Stages) == 0 {
		return fmt.Errorf("workflow %q: at least one stage is required", c.Key())
	}
	if _, err := parseTimeout(c.Timeout); err != nil {
		return fmt.Errorf("workflow %q: timeout %q: %w", c.Key(), c.Timeout, err)
	}

	seen := make(map[string]int, len(c.Stages))
	for i, st := range c.Stages {
		if err := validateStage(c.Key(), i, st); err != nil {
			return err
		}
		if prev, dup := seen[st.Name]; dup {
			return fmt.Errorf("workflow %q: stage %d (%q) repeats the name used by stage %d",
				c.Key(), i+1, st.Name, prev+1)
		}
		seen[st.Name] = i
	}
	return nil
}

func validateStage(key string, idx int, st Stage) error {
	kind := st.Kind()
	if kind == "" {
		return fmt.Errorf("workflow %q: stage %d (%q): exactly one of script/task/workflow is required, found: %s",
			key, idx+1, st.Name, describeStageKeys(st))
	}
	// A field that cannot take effect is a mistake, not a nicety to
	// ignore: someone who wrote `env` on a task stage expected it to
	// reach the process.
	if kind != StageScript {
		var offenders []string
		if len(st.Args) > 0 {
			offenders = append(offenders, "args")
		}
		if len(st.Env) > 0 {
			offenders = append(offenders, "env")
		}
		if len(offenders) > 0 {
			return fmt.Errorf("workflow %q: stage %d (%q): %s only applies to a script stage; a %s stage uses the referenced definition",
				key, idx+1, st.Name, strings.Join(offenders, " and "), kind)
		}
	}
	if _, err := parseTimeout(st.Timeout); err != nil {
		return fmt.Errorf("workflow %q: stage %d (%q): timeout %q: %w", key, idx+1, st.Name, st.Timeout, err)
	}
	return nil
}

// describeStageKeys lists which of the three keys were actually present,
// so the error tells the user what they wrote rather than only what was
// wanted.
func describeStageKeys(st Stage) string {
	var found []string
	if st.Script != "" {
		found = append(found, "script")
	}
	if st.Task != "" {
		found = append(found, "task")
	}
	if st.Workflow != "" {
		found = append(found, "workflow")
	}
	if len(found) == 0 {
		return "none"
	}
	return strings.Join(found, ", ")
}

// TimeoutDuration returns the stage's effective timeout, falling back to
// the workflow's. Zero means no limit.
func (c Config) TimeoutDuration(st Stage) time.Duration {
	if d, err := parseTimeout(st.Timeout); err == nil && d > 0 {
		return d
	}
	d, _ := parseTimeout(c.Timeout)
	return d
}

func parseTimeout(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("must not be negative")
	}
	return d, nil
}

// ValidateAll normalizes nothing but checks a whole declared set:
// each workflow individually, no duplicate keys, and no cycles among
// the workflow stages.
func ValidateAll(cfgs []Config) error {
	defs := make(map[string]Config, len(cfgs))
	for _, c := range cfgs {
		if err := c.Validate(); err != nil {
			return err
		}
		if prev, dup := defs[c.Key()]; dup {
			return fmt.Errorf("workflow %q is declared twice (both in %q and %q)",
				c.Key(), prev.ConfigFile, c.ConfigFile)
		}
		defs[c.Key()] = c
	}
	return CheckAcyclic(defs)
}

// Keys returns the sorted identities of a definition set — used wherever
// output has to be deterministic.
func Keys(defs map[string]Config) []string {
	out := make([]string, 0, len(defs))
	for k := range defs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
