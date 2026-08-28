package wizard

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/bizshuk/pm2/workflow"
)

var stageKindOptions = []string{
	"script   — run a shell command",
	"task     — run a registered task's command once",
	"workflow — run another workflow inline",
}

const otherRefOption = "other (type a reference)"

// askOneWorkflow collects one workflow definition. known is the document
// assembled so far: its apps become the choices for a task stage and its
// workflows the choices for a workflow stage, so the common case never
// requires the user to remember a key.
func askOneWorkflow(
	reader *bufio.Reader,
	output io.Writer,
	known Ecosystem,
) (workflow.Config, error) {
	var wf workflow.Config

	category, err := promptLine(reader, output, "Category", workflow.DefaultCategory)
	if err != nil {
		return wf, err
	}
	wf.Category = strings.TrimSpace(category)

	name, err := promptLine(reader, output, "Name", defaultWorkflowName)
	if err != nil {
		return wf, err
	}
	wf.Name = strings.TrimSpace(name)
	if wf.Name == "" {
		wf.Name = defaultWorkflowName
	}

	cron, err := promptLine(reader, output, "Cron schedule ("+cronOptionPrompt+")", "")
	if err != nil {
		return wf, err
	}
	wf.Cron = resolveCronSchedule(cron)

	wf.Timeout, err = promptDuration(reader, output, "Timeout (e.g. 30m; blank = no limit)")
	if err != nil {
		return wf, err
	}

	cwd, err := promptLine(reader, output, "CWD (blank = ecosystem file directory)", "")
	if err != nil {
		return wf, err
	}
	wf.CWD = strings.TrimSpace(cwd)

	wf.Env, err = promptEnvVars(reader, output)
	if err != nil {
		return wf, err
	}

	wf.Stages, err = collectStages(reader, output, known)
	if err != nil {
		return wf, err
	}
	return wf, nil
}

func collectStages(
	reader *bufio.Reader,
	output io.Writer,
	known Ecosystem,
) ([]workflow.Stage, error) {
	stages := make([]workflow.Stage, 0, 1)
	for number := 1; number <= maxStages; number++ {
		fmt.Fprintf(output, "\n  --- Stage #%d ---\n", number)
		stage, err := askOneStage(reader, output, number, known)
		if err != nil {
			return nil, err
		}
		stages = append(stages, stage)

		if number == maxStages {
			fmt.Fprintf(output, "(reached max of %d stages; stopping)\n", maxStages)
			break
		}
		more, err := promptYesNo(reader, output, "Add another stage?", false)
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
	}
	return stages, nil
}

func askOneStage(
	reader *bufio.Reader,
	output io.Writer,
	number int,
	known Ecosystem,
) (workflow.Stage, error) {
	var stage workflow.Stage

	kind, err := promptChoice(reader, output, "Stage type", stageKindOptions, 1)
	if err != nil {
		return stage, err
	}

	name, err := promptLine(reader, output, "Stage name", fmt.Sprintf("stage-%d", number))
	if err != nil {
		return stage, err
	}
	stage.Name = strings.TrimSpace(name)

	switch kind {
	case 1:
		if err := askScriptStage(reader, output, &stage); err != nil {
			return stage, err
		}
	case 2:
		stage.Task, err = promptRef(reader, output, "Task", known.TaskKeys())
		if err != nil {
			return stage, err
		}
	case 3:
		stage.Workflow, err = promptRef(reader, output, "Workflow", known.WorkflowKeys())
		if err != nil {
			return stage, err
		}
	}

	stage.Timeout, err = promptDuration(
		reader,
		output,
		"Stage timeout (blank = workflow timeout)",
	)
	if err != nil {
		return stage, err
	}
	return stage, nil
}

// askScriptStage collects the fields that belong to a script stage
// alone. Validate rejects args/env on a task or workflow stage, so the
// prompts are not merely skipped there — asking would invite an answer
// the file cannot legally carry.
func askScriptStage(reader *bufio.Reader, output io.Writer, stage *workflow.Stage) error {
	script, err := promptRequiredLine(reader, output, "Script")
	if err != nil {
		return err
	}
	stage.Script = script

	args, err := promptLine(reader, output, "Args (space-separated)", "")
	if err != nil {
		return err
	}
	stage.Args = strings.Fields(args)

	stage.Env, err = promptEnvVars(reader, output)
	if err != nil {
		return err
	}

	cwd, err := promptLine(reader, output, "Stage CWD (blank = workflow cwd)", "")
	if err != nil {
		return err
	}
	stage.CWD = strings.TrimSpace(cwd)
	return nil
}

// promptRef offers the references the document already knows as a
// numbered menu, with a final entry for anything registered elsewhere.
// With nothing to offer it degrades to a plain line prompt rather than a
// one-item menu.
func promptRef(
	reader *bufio.Reader,
	output io.Writer,
	label string,
	candidates []string,
) (string, error) {
	if len(candidates) == 0 {
		return promptRequiredLine(reader, output, label)
	}

	options := append(append([]string{}, candidates...), otherRefOption)
	choice, err := promptChoice(reader, output, label, options, 1)
	if err != nil {
		return "", err
	}
	if choice <= len(candidates) {
		return candidates[choice-1], nil
	}
	return promptRequiredLine(reader, output, label+" reference")
}
