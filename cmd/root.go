package cmd

import (
	"fmt"
	"os"
	"path/filepath"
)

var pm2Home string

func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot determine home dir:", err)
		os.Exit(1)
	}
	pm2Home = filepath.Join(home, ".pm2")
	_ = os.MkdirAll(pm2Home, 0755)
}

func socketPath() string {
	return filepath.Join(pm2Home, "pm2.sock")
}
