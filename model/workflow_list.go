package model

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bizshuk/pm2/workflow"
)

// ListWorkflows asks the daemon for its declared workflows and whatever
// is known about each one's latest run.
//
// It sits beside ListProcesses for the same reason: both the CLI and the
// TUI need it, neither may import the other, and — like the process list
// — an observer asking "what is declared" must never spawn a daemon in
// order to answer.
func ListWorkflows(socketPath string) ([]workflow.Status, error) {
	response, err := SendRequest(socketPath, Request{Command: CmdWorkflowList})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("daemon: empty response to workflow_list")
	}
	if !response.OK {
		return nil, fmt.Errorf("daemon: %s", response.Error)
	}

	var list []workflow.Status
	if err := json.Unmarshal(response.Payload, &list); err != nil {
		return nil, fmt.Errorf("decode workflow list: %w", err)
	}
	return list, nil
}
