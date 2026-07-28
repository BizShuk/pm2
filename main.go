package main

import (
	"os"

	appcmd "github.com/bizshuk/pm2/cmd"
)

func main() {
	if err := appcmd.Execute(os.Args[1:]); err != nil {
		os.Exit(1)
	}
}
