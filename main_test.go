package main

import (
	"path/filepath"
	"testing"

	sdkcmd "github.com/bizshuk/gosdk/cmd"
	sdkconfig "github.com/bizshuk/gosdk/config"
)

func TestRootCmdRegistersConfigCmd(t *testing.T) {
	command, _, err := RootCmd.Find([]string{"config"})
	if err != nil {
		t.Fatalf("find config command: %v", err)
	}
	if command != sdkcmd.ConfigCmd {
		t.Fatalf("config command = %p, want gosdk ConfigCmd %p", command, sdkcmd.ConfigCmd)
	}
}

func TestRootCmdInitializesAppConfigDir(t *testing.T) {
	if got := sdkconfig.GetAppName(); got != "pm2" {
		t.Fatalf("app name = %q, want pm2", got)
	}
	dir := sdkconfig.GetAppConfigDir()
	if dir == "" || filepath.Base(dir) != "pm2" {
		t.Fatalf("app config dir = %q, want ~/.config/pm2", dir)
	}
}
