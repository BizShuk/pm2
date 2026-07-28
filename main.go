package main

import (
	"os"

	rootcmd "github.com/bizshuk/pm2/cmd/root"
)

func main() {
	if err := rootcmd.Execute(os.Args[1:]); err != nil {
		os.Exit(1)
	}
}
