package model

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bizshuk/pm2/process"
)

// TestRequestRoundTrip pins down the on-wire JSON shape of Request.
// If any json tag changes here, it changes the RPC contract with
// every running daemon — we want to catch that in CI, not at 3am
// when a CLI upgrade starts failing on real user machines.
func TestRequestRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		req  Request
		want string
	}{
		{
			name: "minimal ping",
			req:  Request{Command: CmdPing},
			want: `{"command":"ping"}`,
		},
		{
			name: "stop by name",
			req:  Request{Command: CmdStop, Name: "api"},
			want: `{"command":"stop","name":"api"}`,
		},
		{
			name: "restart by id",
			req:  Request{Command: CmdRestart, ID: 7},
			want: `{"command":"restart","id":7}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data, err := json.Marshal(c.req)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if got := string(data); got != c.want {
				t.Errorf("wire shape changed\n  got:  %s\n  want: %s", got, c.want)
			}
			var back Request
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if back.Command != c.req.Command || back.Name != c.req.Name ||
				back.ID != c.req.ID {
				t.Errorf("round-trip lost fields: got %+v want %+v", back, c.req)
			}
		})
	}
}

// TestAppStartReqRoundTrip locks down the wire shape used by
// `pm2 start` / `pm2 save` / `pm2 resurrect`. Schema drift here
// breaks dump.json compatibility with existing installations.
func TestAppStartReqRoundTrip(t *testing.T) {
	req := &AppStartReq{
		AppConfig: process.AppConfig{
			Namespace:   "production",
			Name:        "api",
			Script:      "/usr/bin/node",
			Args:        []string{"server.js", "--port", "8080"},
			Env:         map[string]string{"NODE_ENV": "prod"},
			CronRestart: "@every 1h",
			Cron:        "0 2 * * *",
			Instances:   4,
			MaxRestarts: 10,
			Version:     "1.2.3",
			LogFile:     "/var/log/api.log",
			OutFile:     "/var/log/api-out.log",
			ErrorFile:   "/var/log/api-err.log",
			ConfigDir:   "/etc/pm2",
			Watch:       true,
			ConfigFile:  "/etc/pm2/ecosystem.js",
			CWD:         "/srv/api",
			BaseEnv:     []string{"PATH=/usr/bin", "HOME=/root"},
		},
		CronTriggered: true,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Spot-check key tag spellings; full snapshot would be brittle
	// if we add a field later.
	for _, want := range []string{
		`"cron_restart":"@every 1h"`,
		`"cron_triggered":true`,
		`"max_restarts":10`,
		`"base_env":["PATH=/usr/bin","HOME=/root"]`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing tag %q in %s", want, data)
		}
	}

	var back AppStartReq
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Namespace != req.Namespace || back.Name != req.Name ||
		back.CronTriggered != req.CronTriggered || back.MaxRestarts != req.MaxRestarts ||
		len(back.BaseEnv) != len(req.BaseEnv) {
		t.Errorf("round-trip mismatch:\n  got  %+v\n  want %+v", back, *req)
	}
}

// TestResponseShape verifies the OK / Error / Payload trio.
func TestResponseShape(t *testing.T) {
	// OK-only response omits error and payload.
	r := Response{OK: true}
	data, _ := json.Marshal(r)
	if got := string(data); got != `{"ok":true}` {
		t.Errorf("ok-only shape changed: %s", got)
	}
	// Error response carries error.
	r = Response{Error: "boom"}
	data, _ = json.Marshal(r)
	if !strings.Contains(string(data), `"error":"boom"`) {
		t.Errorf("error shape: %s", data)
	}
}

// TestDialMissingSocket checks the user-facing error when the daemon
// isn't running. The CLI uses this to tell users to start pm2 daemon.
func TestDialMissingSocket(t *testing.T) {
	nonexistent := filepath.Join(t.TempDir(), "no-such-socket")
	_, err := Dial(nonexistent)
	if err == nil {
		t.Fatalf("Dial on missing socket should fail")
	}
	if !strings.Contains(err.Error(), "daemon not running") {
		t.Errorf("error should hint 'daemon not running': %v", err)
	}
}

// TestSendRequestRoundTrip drives SendRequest end-to-end against a
// stub unix-socket server so we know the wire encoding actually
// matches what a real daemon speaks.
func TestSendRequestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	// Server side: accept one connection, decode the request, send
	// a canned response, then close.
	ready := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ln, err := net.Listen("unix", sockPath)
		if err != nil {
			t.Errorf("listen: %v", err)
			return
		}
		close(ready)
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var got Request
		if err := ReadJSON(conn, &got); err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		if got.Command != CmdList || got.Name != "alpha" {
			t.Errorf("server got wrong request: %+v", got)
		}
		_ = WriteJSON(conn, Response{OK: true, Payload: json.RawMessage(`[{"id":1}]`)})
	}()

	<-ready
	resp, err := SendRequest(sockPath, Request{Command: CmdList, Name: "alpha"})
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if !resp.OK {
		t.Errorf("response not OK: %+v", resp)
	}
	if string(resp.Payload) != `[{"id":1}]` {
		t.Errorf("payload: %s", resp.Payload)
	}
	<-done
	_ = os.Remove(sockPath) // avoid leftover socket on slow filesystems
}