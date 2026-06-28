package daemon

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// buildCommand assembles the *exec.Cmd for a managed process.
// `script` + `args` are joined into a single bash command line so that
// shell metacharacters ($VAR, globs, pipes) are interpreted by the shell.
//
// `base` is the snapshot of the user's environment to inherit (the CLI
// passes one in via req.BaseEnv; otherwise the daemon's own environ is used).
// `extra` is req.Env (per-app overrides). `workDir` sets cmd.Dir and $PWD.
func buildCommand(script string, args, base []string, extra map[string]string, workDir string) *exec.Cmd {
	shellCmd := strings.Join(append([]string{script}, args...), " ")
	cmd := exec.Command("/bin/bash", "-c", shellCmd)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if workDir != "" {
		cmd.Dir = workDir
	}

	// Drop inherited PWD; cmd.Dir is the single source of truth.
	cmd.Env = make([]string, 0, len(base)+len(extra)+1)
	for _, kv := range base {
		if strings.HasPrefix(kv, "PWD=") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	for k, v := range extra {
		if k == "PWD" {
			continue
		}
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	pwd := workDir
	if pwd == "" {
		pwd, _ = os.Getwd()
	}
	if pwd != "" {
		cmd.Env = append(cmd.Env, "PWD="+pwd)
	}
	return cmd
}