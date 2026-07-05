package network

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/bizshuk/pm2/model"
)

// Handle reads one Request from conn, dispatches to the matching
// Manager method, and writes one Response. The connection is closed
// on return.
//
// CmdKill is special: it triggers os.Exit after the response is
// flushed, matching the original Server.handleConn behavior (the
// daemon exits once the kill request is acknowledged).
func Handle(conn net.Conn, m Manager) {
	defer conn.Close()

	var req model.Request
	if err := model.ReadJSON(conn, &req); err != nil {
		_ = model.WriteJSON(conn, model.Response{Error: err.Error()})
		return
	}

	resp := dispatch(req, m)
	_ = model.WriteJSON(conn, resp)

	// Post-response hook: CmdKill schedules an os.Exit after the
	// reply is flushed. The small delay lets WriteJSON complete on
	// its own goroutine context.
	if req.Command == model.CmdKill {
		go func() {
			time.Sleep(150 * time.Millisecond)
			slog.Info("daemon shutting down via kill command")
			os.Exit(0)
		}()
	}
}

// dispatch is the per-command switch — kept as a free function so the
// public Handle entry point is small and the dispatch logic is unit-
// testable in isolation.
func dispatch(req model.Request, m Manager) model.Response {
	switch req.Command {
	case model.CmdPing:
		m.Ping()
		return model.Response{OK: true}

	case model.CmdStatus:
		info := m.Status()
		payload, _ := json.Marshal(info)
		return model.Response{OK: true, Payload: payload}

	case model.CmdStart:
		if req.App == nil {
			return model.Response{Error: "missing app config"}
		}
		infos, err := m.StartApp(req.App)
		if err != nil {
			return model.Response{Error: err.Error()}
		}
		payload, _ := json.Marshal(infos)
		return model.Response{OK: true, Payload: payload}

	case model.CmdStop:
		if err := m.StopByName(req.Name); err != nil {
			return model.Response{Error: err.Error()}
		}
		return model.Response{OK: true}

	case model.CmdRestart:
		if err := m.RestartByName(req.Name); err != nil {
			return model.Response{Error: err.Error()}
		}
		return model.Response{OK: true}

	case model.CmdPause:
		if err := m.PauseByName(req.Name); err != nil {
			return model.Response{Error: err.Error()}
		}
		return model.Response{OK: true}

	case model.CmdResume:
		if err := m.ResumeByName(req.Name); err != nil {
			return model.Response{Error: err.Error()}
		}
		return model.Response{OK: true}

	case model.CmdDelete:
		if err := m.DeleteByName(req.Name); err != nil {
			return model.Response{Error: err.Error()}
		}
		return model.Response{OK: true}

	case model.CmdList:
		infos := m.ListAll()
		payload, _ := json.Marshal(infos)
		return model.Response{OK: true, Payload: payload}

	case model.CmdSave:
		if err := m.Save(); err != nil {
			return model.Response{Error: err.Error()}
		}
		return model.Response{OK: true}

	case model.CmdResurrect:
		if err := m.Resurrect(); err != nil {
			return model.Response{Error: err.Error()}
		}
		return model.Response{OK: true}

	case model.CmdKill:
		// Gracefully stop every managed process; Handle's
		// post-response hook will schedule os.Exit.
		m.KillAll()
		return model.Response{OK: true}

	default:
		return model.Response{Error: fmt.Sprintf("unknown command: %s", req.Command)}
	}
}