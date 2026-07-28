package wizard

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/bizshuk/pm2/process"
)

var (
	namespaceOptions = []string{"Agent", "Backup", "Local", "Service", "AutoP"}
	optionalOptions  = []string{"Yes (registered but paused)", "No"}
)

// Namespaces returns the wizard's supported namespace choices in prompt order.
func Namespaces() []string {
	return slices.Clone(namespaceOptions)
}

// DefaultApp returns one AppConfig populated with interactive defaults.
func DefaultApp() process.AppConfig {
	app := process.AppConfig{
		Script:    defaultScript,
		Name:      defaultName,
		Namespace: namespaceOptions[0],
		Instances: process.DefaultInstances,
		Version:   DefaultVersion,
		Optional:  true,
	}
	app.Name = formatWizardName(app.Namespace, app.Script, app.Name)
	app.Normalize("")
	return app
}

func askOneApp(reader *bufio.Reader, output io.Writer) (process.AppConfig, error) {
	var app process.AppConfig

	namespaceChoice, err := promptChoice(reader, output, "Namespace", namespaceOptions, 1)
	if err != nil {
		return app, err
	}
	app.Namespace = namespaceOptions[namespaceChoice-1]

	app.Name, err = promptLine(reader, output, "Name", defaultName)
	if err != nil {
		return app, err
	}
	if app.Name == "" {
		app.Name = defaultName
	}
	app.Script, err = promptLine(reader, output, "Script", defaultScript)
	if err != nil {
		return app, err
	}
	if app.Script == "" {
		app.Script = defaultScript
	}
	if _, err := os.Stat(app.Script); err != nil {
		fmt.Fprintf(output, "  (warning: %q not found locally — continuing anyway)\n", app.Script)
	}
	app.Name = formatWizardName(app.Namespace, app.Script, app.Name)

	args, err := promptLine(reader, output, "Args (space-separated)", "")
	if err != nil {
		return app, err
	}
	app.Args = strings.Fields(args)

	app.Instances, err = promptInstances(reader, output)
	if err != nil {
		return app, err
	}
	app.Watch, err = promptYesNo(reader, output, "Watch mode?", false)
	if err != nil {
		return app, err
	}
	app.Env, err = promptEnvVars(reader, output)
	if err != nil {
		return app, err
	}

	cron, err := promptLine(reader, output, "Cron schedule ("+cronOptionPrompt+")", "")
	if err != nil {
		return app, err
	}
	app.Cron = resolveCronSchedule(cron)

	if err := promptAdditionalAppOptions(reader, output, &app); err != nil {
		return app, err
	}

	optionalChoice, err := promptChoice(reader, output, "Optional", optionalOptions, 1)
	if err != nil {
		return app, err
	}
	app.Optional = optionalChoice == 1
	app.Version = DefaultVersion
	return app, nil
}
