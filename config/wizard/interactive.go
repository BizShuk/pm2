package wizard

import (
	"bufio"

	"github.com/bizshuk/pm2/process"
)

// RunInteractive collects apps or accepts defaults, then writes the ecosystem
// file using the selected merge and format behavior.
func RunInteractive(ctx WizardContext, opts WriteOptions) error {
	var apps []process.AppConfig
	if ctx.YesAll {
		apps = []process.AppConfig{DefaultApp()}
	} else {
		reader := bufio.NewReader(ctx.In)
		var err error
		apps, err = collectAnswers(reader, ctx.Out)
		if err != nil {
			return err
		}
		ctx.In = reader
	}

	return WriteEcosystemFile(ctx, apps, opts)
}
