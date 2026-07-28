package wizard

import (
	"bufio"
	"fmt"
	"io"

	"github.com/bizshuk/pm2/process"
)

func collectAnswers(input io.Reader, output io.Writer) ([]process.AppConfig, error) {
	reader, ok := input.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(input)
	}

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
