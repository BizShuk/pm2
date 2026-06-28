package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bizshuk/pm2/config"
)

// defaultApp returns a single AppConfig pre-filled with safe defaults.
func defaultApp() config.AppConfig {
	a := config.AppConfig{
		Script:    ecoDefaultScript,
		Name:      ecoDefaultName,
		Namespace: ecoDefaultNS,
		Instances: 1,
		Version:   ecoDefaultVersion,
	}
	a.Normalize()
	return a
}

// promptLine reads a single line, trims whitespace, returns it.
// Empty input == def. EOF returns an error.
func promptLine(rdr *bufio.Reader, out io.Writer, label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}
	line, err := rdr.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	if err == io.EOF && line == "" {
		return "", nil
	}
	line = strings.TrimRight(line, "\r\n")
	return line, nil
}

// promptYesNo accepts y/yes/n/no (case-insensitive). Empty == def.
func promptYesNo(rdr *bufio.Reader, out io.Writer, label string, def bool) (bool, error) {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	fmt.Fprintf(out, "%s [%s]: ", label, hint)
	line, err := rdr.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	line = strings.TrimSpace(strings.TrimRight(line, "\r\n"))
	if line == "" {
		return def, nil
	}
	switch strings.ToLower(line) {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	}
	return def, nil
}

// promptInstances reads an int with a soft retry loop; falls back to 1.
func promptInstances(rdr *bufio.Reader, out io.Writer) (int, error) {
	for i := 0; i < 3; i++ {
		s, err := promptLine(rdr, out, "Instances", "1")
		if err != nil {
			return 0, err
		}
		if s == "" {
			return 1, nil
		}
		n, perr := strconv.Atoi(strings.TrimSpace(s))
		if perr == nil && n > 0 {
			return n, nil
		}
		fmt.Fprintln(out, "  (invalid number, try again)")
	}
	return 1, nil
}

// promptEnvVars loops reading KEY=VAL until blank line.
func promptEnvVars(rdr *bufio.Reader, out io.Writer) (map[string]string, error) {
	env := make(map[string]string)
	fmt.Fprintln(out, "Env vars? (one per line KEY=VAL; blank line to finish)")
	for {
		s, err := promptLine(rdr, out, "  env", "")
		if err != nil {
			return nil, err
		}
		if s == "" {
			break
		}
		parts := strings.SplitN(s, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			fmt.Fprintf(out, "  (ignoring malformed env: %q)\n", s)
			continue
		}
		env[strings.TrimSpace(parts[0])] = parts[1]
	}
	if len(env) == 0 {
		return nil, nil
	}
	return env, nil
}

// collectAnswers walks the per-app question block and loops on "add another app?".
func collectAnswers(in io.Reader, out io.Writer) ([]config.AppConfig, error) {
	rdr := bufio.NewReader(in)
	var apps []config.AppConfig
	for n := 1; n <= ecoMaxApps; n++ {
		fmt.Fprintf(out, "\n=== App #%d ===\n", n)
		app, err := askOneApp(rdr, out)
		if err != nil {
			return nil, err
		}
		app.Normalize()
		apps = append(apps, app)
		fmt.Fprintf(out, "  -> app #%d: name=%s script=%s instances=%d namespace=%s watch=%t cron=%q\n",
			n, app.Name, app.Script, app.Instances, app.Namespace, app.Watch, app.Cron)
		if n == ecoMaxApps {
			fmt.Fprintf(out, "(reached max of %d apps; stopping)\n", ecoMaxApps)
			break
		}
		more, err := promptYesNo(rdr, out, "Add another app?", false)
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
	}
	return apps, nil
}

// askOneApp runs the per-app question block for a single AppConfig.
func askOneApp(rdr *bufio.Reader, out io.Writer) (config.AppConfig, error) {
	var app config.AppConfig

	script, err := promptLine(rdr, out, "Script path", ecoDefaultScript)
	if err != nil {
		return app, err
	}
	if script == "" {
		script = ecoDefaultScript
	}
	if _, err := os.Stat(script); err != nil {
		fmt.Fprintf(out, "  (warning: %q not found locally — continuing anyway)\n", script)
	}
	app.Script = script

	name, err := promptLine(rdr, out, "Process name", deriveName(script))
	if err != nil {
		return app, err
	}
	if name == "" {
		name = deriveName(script)
	}
	app.Name = name

	argsRaw, err := promptLine(rdr, out, "Args (space-separated)", "")
	if err != nil {
		return app, err
	}
	if argsRaw != "" {
		app.Args = strings.Fields(argsRaw)
	}

	ns, err := promptLine(rdr, out, "Namespace", ecoDefaultNS)
	if err != nil {
		return app, err
	}
	if ns == "" {
		ns = ecoDefaultNS
	}
	app.Namespace = ns

	inst, err := promptInstances(rdr, out)
	if err != nil {
		return app, err
	}
	app.Instances = inst

	watch, err := promptYesNo(rdr, out, "Watch mode?", false)
	if err != nil {
		return app, err
	}
	app.Watch = watch

	env, err := promptEnvVars(rdr, out)
	if err != nil {
		return app, err
	}
	app.Env = env

	cron, err := promptLine(rdr, out, "Cron schedule (e.g. \"*/5 * * * *\", blank to skip)", "")
	if err != nil {
		return app, err
	}
	if cron != "" {
		app.Cron = cron
		restart, err := promptYesNo(rdr, out, "Cron restart (re-spawn the process on this schedule)?", false)
		if err != nil {
			return app, err
		}
		if restart {
			app.CronRestart = cron
		}
	}

	app.Version = ecoDefaultVersion
	return app, nil
}

// deriveName produces a process name from a script path, matching
// config.AppConfig.Normalize (cmd/ecosystem.go:50-53).
func deriveName(script string) string {
	if script == "" {
		return ecoDefaultName
	}
	base := filepath.Base(script)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	if base == "" {
		return ecoDefaultName
	}
	return base
}