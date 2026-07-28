package wizard

import "github.com/bizshuk/pm2/process"

// DefaultInstallOptions returns the default install options.
func DefaultInstallOptions() WriteOptions {
	return DefaultWriteOptions()
}

// RunInstall writes one pre-built app without interactive confirmation.
func RunInstall(ctx WizardContext, app process.AppConfig, opts WriteOptions) error {
	ctx.YesAll = true
	return WriteEcosystemFile(ctx, []process.AppConfig{app}, opts)
}
