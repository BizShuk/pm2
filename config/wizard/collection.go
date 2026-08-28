package wizard

import (
	"bufio"
	"fmt"
	"io"

	"github.com/bizshuk/pm2/process"
	"github.com/bizshuk/pm2/workflow"
)

func collectAnswers(input io.Reader, output io.Writer) (Ecosystem, error) {
	reader, ok := input.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(input)
	}

	var doc Ecosystem
	apps, err := collectApps(reader, output)
	if err != nil {
		return doc, err
	}
	doc.Apps = apps

	// Workflows are collected after the apps, in the same order the
	// daemon registers them: a task stage picks from the apps this file
	// declares, so asking first would leave the picker empty.
	doc.Workflows, err = collectWorkflows(reader, output, doc)
	if err != nil {
		return doc, err
	}
	return doc, nil
}

func collectApps(reader *bufio.Reader, output io.Writer) ([]process.AppConfig, error) {
	apps := make([]process.AppConfig, 0, 1)
	for number := 1; number <= maxApps; number++ {
		fmt.Fprintf(output, "\n=== App #%d ===\n", number)
		app, err := askOneApp(reader, output)
		if err != nil {
			return nil, err
		}
		app.Normalize("")
		apps = append(apps, app)
		writeAppSummary(output, number, app)

		if number == maxApps {
			fmt.Fprintf(output, "(reached max of %d apps; stopping)\n", maxApps)
			break
		}
		more, err := promptYesNo(reader, output, "Add another app?", false)
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
	}
	return apps, nil
}

func collectWorkflows(
	reader *bufio.Reader,
	output io.Writer,
	doc Ecosystem,
) ([]workflow.Config, error) {
	fmt.Fprintln(output, "\nA workflow runs stages in order and stops at the first failure.")
	add, err := promptYesNo(reader, output, "Add a workflow?", false)
	if err != nil || !add {
		return nil, err
	}

	workflows := make([]workflow.Config, 0, 1)
	for number := 1; number <= maxWorkflows; number++ {
		fmt.Fprintf(output, "\n=== Workflow #%d ===\n", number)
		wf, err := askOneWorkflow(reader, output, doc)
		if err != nil {
			return nil, err
		}
		workflows = append(workflows, wf)
		doc.Workflows = workflows
		writeWorkflowSummary(output, number, wf)

		if number == maxWorkflows {
			fmt.Fprintf(output, "(reached max of %d workflows; stopping)\n", maxWorkflows)
			break
		}
		more, err := promptYesNo(reader, output, "Add another workflow?", false)
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
	}
	return workflows, nil
}

func writeAppSummary(output io.Writer, number int, app process.AppConfig) {
	fmt.Fprintf(
		output,
		"  -> app #%d: name=%s script=%s instances=%d namespace=%s watch=%t cron=%q cron_restart=%q max_restarts=%d optional=%t\n",
		number,
		app.Name,
		app.Script,
		app.Instances,
		app.Namespace,
		app.Watch,
		app.Cron,
		app.CronRestart,
		app.MaxRestarts,
		app.Optional,
	)
}

func writeWorkflowSummary(output io.Writer, number int, wf workflow.Config) {
	category := wf.Category
	if category == "" {
		category = workflow.DefaultCategory
	}
	fmt.Fprintf(
		output,
		"  -> workflow #%d: key=%s:%s stages=%d cron=%q timeout=%q\n",
		number,
		category,
		wf.Name,
		len(wf.Stages),
		wf.Cron,
		wf.Timeout,
	)
	for i, st := range wf.Stages {
		fmt.Fprintf(
			output,
			"     stage %d: name=%s kind=%s ref=%q\n",
			i+1,
			st.Name,
			stageKindLabel(st),
			stageTarget(st),
		)
	}
}

func stageKindLabel(st workflow.Stage) string {
	if kind := st.Kind(); kind != "" {
		return string(kind)
	}
	return "invalid"
}

func stageTarget(st workflow.Stage) string {
	if st.Kind() == workflow.StageScript {
		return st.Script
	}
	return st.Ref()
}
