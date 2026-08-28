package wizard

import (
	"bufio"

	"github.com/bizshuk/pm2/process"
)

// RunInteractive collects apps and workflows, or accepts defaults, then
// writes the ecosystem file using the selected merge and format
// behavior.
//
// The --yes path deliberately synthesizes no workflow: a default app is
// a runnable placeholder, but a workflow with an invented stage would be
// a command nobody asked to run.
func RunInteractive(ctx WizardContext, opts WriteOptions) error {
	doc := Ecosystem{Apps: []process.AppConfig{DefaultApp()}}
	if !ctx.YesAll {
		reader := bufio.NewReader(ctx.In)
		collected, err := collectAnswers(reader, ctx.Out)
		if err != nil {
			return err
		}
		doc = collected
		ctx.In = reader
	}

	return WriteEcosystemFile(ctx, doc, opts)
}
