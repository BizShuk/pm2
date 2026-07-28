package root

import (
	"fmt"

	"github.com/bizshuk/pm2/model"
)

// Execute runs the customized PM2 root command with args.
func Execute(args []string) error {
	if len(args) > 0 && isVersionArg(args[0]) {
		fmt.Fprintln(Cmd.OutOrStdout(), model.PM2Version)
		return nil
	}

	Cmd.SetArgs(args)
	return Cmd.Execute()
}

func isVersionArg(arg string) bool {
	switch arg {
	case "version", "-v", "--v", "--version", "-version":
		return true
	default:
		return false
	}
}
