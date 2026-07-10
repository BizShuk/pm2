package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
)

func doRefresh(socket string) tea.Cmd {
	return func() tea.Msg {
		resp, err := model.SendRequest(socket, model.Request{Command: model.CmdList})
		if err != nil {
			return refreshMsg{err: err}
		}
		var procs []process.ProcessInfo
		if err := json.Unmarshal(resp.Payload, &procs); err != nil {
			return refreshMsg{err: err}
		}
		sort.Slice(procs, func(i, j int) bool { return procs[i].ID < procs[j].ID })
		return refreshMsg{procs: procs}
	}
}

func readLogs(path string) tea.Cmd {
	return func() tea.Msg {
		f, err := os.Open(path)
		if err != nil {
			return logsMsg{path: path}
		}
		defer f.Close()
		var lines []string
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
		if err := sc.Err(); err != nil {
			// Ignore or log error
		}
		if len(lines) > maxLogTail {
			lines = lines[len(lines)-maxLogTail:]
		}
		return logsMsg{path: path, lines: lines}
	}
}

// doAction sends an RPC then immediately re-fetches the process list.
// The action's outcome is threaded back so the UI can report a failure
// instead of silently swallowing it — e.g. a stale daemon that does not
// recognise `pause`/`resume` replies "unknown command", which would
// otherwise leave the status looking unchanged with no explanation.
func doAction(socket string, req model.Request) tea.Cmd {
	return func() tea.Msg {
		var notice string
		resp, err := model.SendRequest(socket, req)
		switch {
		case err != nil:
			notice = fmt.Sprintf("%s failed: %v", req.Command, err)
		case resp != nil && !resp.OK:
			notice = fmt.Sprintf("%s failed: %s", req.Command, resp.Error)
		}
		refresh := doRefresh(socket)().(refreshMsg)
		return actionMsg{refreshMsg: refresh, notice: notice}
	}
}
