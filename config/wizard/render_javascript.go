package wizard

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/bizshuk/pm2/process"
)

func renderEcosystemJS(apps []process.AppConfig) string {
	var output strings.Builder
	output.WriteString("module.exports = {\n")
	output.WriteString("    apps: [\n")
	for i, app := range apps {
		writeAppJS(&output, appForRender(app))
		if i < len(apps)-1 {
			output.WriteString(",")
		}
		output.WriteString("\n")
	}
	output.WriteString("    ],\n")
	output.WriteString("};\n")
	return output.String()
}

func writeAppJS(output *strings.Builder, app renderedApp) {
	namespace := app.Namespace
	if namespace == "" {
		namespace = process.DefaultNamespace
	}
	fmt.Fprintf(output, "        // %s (%s)\n", app.Name, namespace)

	output.WriteString("        {\n")
	writeJSString(output, "name", app.Name)
	fmt.Fprintf(output, "            script: %s,\n", strconv.Quote(app.Script))
	if len(app.Args) > 0 {
		output.WriteString("            args: [")
		for i, arg := range app.Args {
			if i > 0 {
				output.WriteString(", ")
			}
			output.WriteString(strconv.Quote(arg))
		}
		output.WriteString("],\n")
	}
	writeJSString(output, "namespace", app.Namespace)
	writeJSString(output, "cwd", app.CWD)
	fmt.Fprintf(output, "            instances: %d,\n", app.Instances)
	if app.Watch {
		output.WriteString("            watch: true,\n")
	}
	writeJSEnv(output, app.Env)
	writeJSString(output, "cron_restart", app.CronRestart)
	writeJSString(output, "cron", app.Cron)
	fmt.Fprintf(output, "            max_restarts: %d,\n", app.MaxRestarts)
	writeJSString(output, "config_dir", app.ConfigDir)
	writeJSString(output, "log_file", app.LogFile)
	writeJSString(output, "out_file", app.OutFile)
	writeJSString(output, "error_file", app.ErrorFile)
	if app.Optional {
		output.WriteString("            optional: true,\n")
	}
	output.WriteString("        }")
}

func writeJSString(output *strings.Builder, key, value string) {
	if value != "" {
		fmt.Fprintf(output, "            %s: %s,\n", key, strconv.Quote(value))
	}
}

func writeJSEnv(output *strings.Builder, env map[string]string) {
	if len(env) == 0 {
		return
	}

	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	output.WriteString("            env: {\n")
	for i, key := range keys {
		fmt.Fprintf(output, "                %s: %s", strconv.Quote(key), strconv.Quote(env[key]))
		if i < len(keys)-1 {
			output.WriteString(",")
		}
		output.WriteString("\n")
	}
	output.WriteString("            },\n")
}
