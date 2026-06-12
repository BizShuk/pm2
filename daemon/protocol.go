package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
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

// AppStartReq carries the config for a new process
type AppStartReq struct {
	Namespace   string            `json:"namespace"`
	Name        string            `json:"name"`
	Script      string            `json:"script"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	CronRestart string            `json:"cron_restart"`
	Instances   int               `json:"instances"`
	MaxRestarts int               `json:"max_restarts"`
	LogFile     string            `json:"log_file"`
	OutFile     string            `json:"out_file"`
	ErrorFile   string            `json:"error_file"`
	ConfigDir   string            `json:"config_dir"`
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
