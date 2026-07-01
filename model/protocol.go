// Package model holds the cross-package data contracts that travel
// between the CLI, the daemon, and any other process that wants to
// speak to pm2. It contains pure types and the JSON wire helpers —
// no business logic, no side effects beyond the network dial.
//
// Lives outside the daemon/ subtree on purpose: cmd/ and tui/ import
// model/ to talk to a running daemon without dragging in the entire
// server (process registry, executor, network listener, etc.). A
// process that only needs to send RPC requests should depend on
// model/ alone.
package model

import (
	"encoding/json"
	"fmt"
	"io"
	"net"

	"github.com/bizshuk/pm2/process"
)

// CommandType enumerates RPC commands sent from CLI to daemon
type CommandType string

const (
	CmdStart    CommandType = "start"
	CmdStop     CommandType = "stop"
	CmdRestart  CommandType = "restart"
	CmdDelete   CommandType = "delete"
	CmdList     CommandType = "list"
	CmdLogs     CommandType = "logs"
	CmdSave     CommandType = "save"
	CmdResurrect CommandType = "resurrect"
	CmdKill     CommandType = "kill"
	CmdPing     CommandType = "ping"
)

// Request is a CLI → daemon message
type Request struct {
	Command CommandType `json:"command"`
	Name    string      `json:"name,omitempty"` // process name or "all"
	ID      int         `json:"id,omitempty"`
	App     *AppStartReq `json:"app,omitempty"`
	Follow  bool        `json:"follow,omitempty"`
}

// AppStartReq carries the config for a new process. The static
// configuration fields (name/script/args/env/...) are inherited from
// process.AppConfig via anonymous embedding; CronTriggered is the only
// transport-layer addition (the daemon needs to know whether the
// request was triggered by cron so it can avoid re-registering the
// cron entry on every restart).
type AppStartReq struct {
	process.AppConfig
	CronTriggered bool `json:"cron_triggered"`
}

// Response is a daemon → CLI message
type Response struct {
	OK      bool            `json:"ok"`
	Error   string          `json:"error,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// WriteJSON sends a JSON-encoded value over conn with a newline delimiter
func WriteJSON(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// ReadJSON decodes one newline-delimited JSON message from conn
func ReadJSON(r io.Reader, v any) error {
	dec := json.NewDecoder(r)
	return dec.Decode(v)
}

// Dial connects to the running daemon; returns error if daemon is not running
func Dial(socketPath string) (net.Conn, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("daemon not running (start with: pm2 daemon): %w", err)
	}
	return conn, nil
}

// SendRequest sends a request and reads back the response
func SendRequest(socketPath string, req Request) (*Response, error) {
	conn, err := Dial(socketPath)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := WriteJSON(conn, req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	var resp Response
	if err := ReadJSON(conn, &resp); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return &resp, nil
}
