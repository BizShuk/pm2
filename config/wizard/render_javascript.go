package wizard

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/bizshuk/pm2/process"
)

const (
	appIndent   = "        "
	fieldIndent = "            "
	stageIndent = "                "
	stageField  = "                    "
)

func renderEcosystemJS(doc Ecosystem) string {
	var output strings.Builder
	output.WriteString("module.exports = {\n")
	output.WriteString("    apps: [\n")
	for i, app := range doc.Apps {
		writeAppJS(&output, appForRender(app))
		if i < len(doc.Apps)-1 {
			output.WriteString(",")
		}
		output.WriteString("\n")
	}
	output.WriteString("    ],\n")

	// The block is omitted entirely when there is nothing to declare: an
	// empty `workflows: []` in every generated file would read as a
	// feature the user has to opt out of.
	if len(doc.Workflows) > 0 {
		output.WriteString("    workflows: [\n")
		for i, wf := range doc.Workflows {
			writeWorkflowJS(&output, workflowForRender(wf))
			if i < len(doc.Workflows)-1 {
				output.WriteString(",")
			}
			output.WriteString("\n")
		}
		output.WriteString("    ],\n")
	}

	output.WriteString("};\n")
	return output.String()
}

func writeAppJS(output *strings.Builder, app renderedApp) {
	namespace := app.Namespace
	if namespace == "" {
		namespace = process.DefaultNamespace
	}
	fmt.Fprintf(output, "%s// %s (%s)\n", appIndent, app.Name, namespace)

	output.WriteString(appIndent + "{\n")
	writeJSString(output, fieldIndent, "name", app.Name)
	fmt.Fprintf(output, "%sscript: %s,\n", fieldIndent, strconv.Quote(app.Script))
	writeJSArgs(output, fieldIndent, app.Args)
	writeJSString(output, fieldIndent, "namespace", app.Namespace)
	writeJSString(output, fieldIndent, "cwd", app.CWD)
	fmt.Fprintf(output, "%sinstances: %d,\n", fieldIndent, app.Instances)
	if app.Watch {
		output.WriteString(fieldIndent + "watch: true,\n")
	}
	writeJSEnv(output, fieldIndent, app.Env)
	writeJSString(output, fieldIndent, "cron_restart", app.CronRestart)
	writeJSString(output, fieldIndent, "cron", app.Cron)
	fmt.Fprintf(output, "%smax_restarts: %d,\n", fieldIndent, app.MaxRestarts)
	if app.Optional {
		output.WriteString(fieldIndent + "optional: true,\n")
	}
	output.WriteString(appIndent + "}")
}

func writeWorkflowJS(output *strings.Builder, wf renderedWorkflow) {
	fmt.Fprintf(output, "%s// %s:%s\n", appIndent, wf.Category, wf.Name)

	output.WriteString(appIndent + "{\n")
	writeJSString(output, fieldIndent, "name", wf.Name)
	writeJSString(output, fieldIndent, "category", wf.Category)
	writeJSString(output, fieldIndent, "cron", wf.Cron)
	writeJSString(output, fieldIndent, "timeout", wf.Timeout)
	writeJSString(output, fieldIndent, "cwd", wf.CWD)
	writeJSEnv(output, fieldIndent, wf.Env)

	output.WriteString(fieldIndent + "stages: [\n")
	for i, st := range wf.Stages {
		output.WriteString(stageIndent + "{\n")
		writeJSString(output, stageField, "name", st.Name)
		writeJSString(output, stageField, "script", st.Script)
		writeJSArgs(output, stageField, st.Args)
		writeJSString(output, stageField, "task", st.Task)
		writeJSString(output, stageField, "workflow", st.Workflow)
		writeJSEnv(output, stageField, st.Env)
		writeJSString(output, stageField, "cwd", st.CWD)
		writeJSString(output, stageField, "timeout", st.Timeout)
		output.WriteString(stageIndent + "}")
		if i < len(wf.Stages)-1 {
			output.WriteString(",")
		}
		output.WriteString("\n")
	}
	output.WriteString(fieldIndent + "],\n")
	output.WriteString(appIndent + "}")
}

func writeJSArgs(output *strings.Builder, indent string, args []string) {
	if len(args) == 0 {
		return
	}
	fmt.Fprintf(output, "%sargs: [", indent)
	for i, arg := range args {
		if i > 0 {
			output.WriteString(", ")
		}
		output.WriteString(strconv.Quote(arg))
	}
	output.WriteString("],\n")
}

func writeJSString(output *strings.Builder, indent, key, value string) {
	if value != "" {
		fmt.Fprintf(output, "%s%s: %s,\n", indent, key, strconv.Quote(value))
	}
}

func writeJSEnv(output *strings.Builder, indent string, env map[string]string) {
	if len(env) == 0 {
		return
	}

	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	fmt.Fprintf(output, "%senv: {\n", indent)
	for i, key := range keys {
		fmt.Fprintf(output, "%s    %s: %s", indent, strconv.Quote(key), strconv.Quote(env[key]))
		if i < len(keys)-1 {
			output.WriteString(",")
		}
		output.WriteString("\n")
	}
	fmt.Fprintf(output, "%s},\n", indent)
}
