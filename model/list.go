package model

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bizshuk/pm2/process"
)

// ListProcesses asks the daemon for its full process list and decodes the
// payload.
//
// It lives in model/ because both the CLI and the TUI need it and neither
// may import the other. Unlike cmd/runtime's CLIClient this never spawns a
// daemon: an observer asking "what is running" must not change the answer.
func ListProcesses(socketPath string) ([]process.ProcessInfo, error) {
	response, err := SendRequest(socketPath, Request{Command: CmdList})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("daemon: empty response to list")
	}
	if !response.OK {
		return nil, fmt.Errorf("daemon: %s", response.Error)
	}

	var managed []process.ProcessInfo
	if err := json.Unmarshal(response.Payload, &managed); err != nil {
		return nil, fmt.Errorf("decode process list: %w", err)
	}
	return managed, nil
}
