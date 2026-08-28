package wizard

import "github.com/bizshuk/pm2/process"

// RunInstall writes one pre-built app without interactive confirmation.
func RunInstall(ctx WizardContext, app process.AppConfig, opts WriteOptions) error {
	ctx.YesAll = true
	return WriteEcosystemFile(ctx, Ecosystem{Apps: []process.AppConfig{app}}, opts)
}
