// Package executor encapsulates the OS-level operations for a single
// managed process: launching (fork+exec), watching (cmd.Wait), stopping
// (signal+kill), file-watching (fsnotify) and metrics collection.
//
// Lock direction (Phase 4 invariant):
//   - The daemon package may call Executor while holding the registry
//     lock, because Executor holds NO lock during its execution.
//   - Executor NEVER calls back into the registry — it only signals
//     via callbacks. The Server's callback implementations take the
//     registry lock internally (via ProcessRegistry.UpdateInfo) and
//     do not hold it across any blocking call.
package executor

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// BuildCommand assembles the *exec.Cmd for a managed process.
// `script` + `args` are joined into a single bash command line so that
// shell metacharacters ($VAR, globs, pipes) are interpreted by the shell.
//
// `base` is the snapshot of the user's environment to inherit (the CLI
// passes one in via req.BaseEnv; otherwise the daemon's own environ is used).
// `extra` is req.Env (per-app overrides). `workDir` sets cmd.Dir and $PWD.
func BuildCommand(script string, args, base []string, extra map[string]string, workDir string) *exec.Cmd {
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