package wizard

import (
	"bufio"
	"io"
	"strings"

	"github.com/bizshuk/pm2/process"
)

const cronOptionPrompt = "blank to skip, r for random daily between 2am and 8am, or enter cron format"

// promptAdditionalAppOptions collects the user-facing AppConfig fields that
// follow the primary namespace/script/runtime questions.
func promptAdditionalAppOptions(
	rdr *bufio.Reader,
	out io.Writer,
	app *process.AppConfig,
) error {
	cronRestart, err := promptLine(
		rdr,
		out,
		"Cron restart ("+cronOptionPrompt+")",
		"",
	)
	if err != nil {
		return err
	}
	app.CronRestart = resolveCronSchedule(cronRestart)

	maxRestarts, err := promptPositiveInt(
		rdr,
		out,
		"Max restarts",
		process.DefaultMaxRestarts,
	)
	if err != nil {
		return err
	}
	app.MaxRestarts = maxRestarts

	cwd, err := promptLine(
		rdr,
		out,
		"CWD (blank = ecosystem file directory)",
		"",
	)
	if err != nil {
		return err
	}
	app.CWD = strings.TrimSpace(cwd)

	return nil
}
