package cmd

import (
	"strings"
	"testing"
)

func TestLogsCommandIsInteractiveBrowser(t *testing.T) {
	if LogsCmd.Use != "logs [name]" {
		t.Fatalf("LogsCmd.Use = %q, want %q", LogsCmd.Use, "logs [name]")
	}
	if flag := LogsCmd.Flags().Lookup("lines"); flag != nil {
		t.Fatalf("LogsCmd still exposes direct-tail flag: %#v", flag)
	}
	if !strings.Contains(strings.ToLower(LogsCmd.Short), "browser") {
		t.Fatalf("LogsCmd.Short = %q, want browser description", LogsCmd.Short)
	}
}
