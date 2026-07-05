// Package network encapsulates the daemon's Unix socket listener and
// per-connection RPC dispatch. The package depends only on the Manager
// interface (defined here) — the daemon package supplies the concrete
// implementation, so:
//   - network never imports daemon
//   - executor / registry never import network
//   - the import graph is strictly: network -> (Manager contract only)
//
// This is the import-cycle guard that lets the network layer be tested
// against a mock Manager and the Manager layer be tested without binding
// a Unix socket.
package network

import (
	"github.com/bizshuk/pm2/model"
	"github.com/bizshuk/pm2/process"
)

// Manager is the surface the RPC dispatcher needs from the daemon
// process. Defined as an interface so:
//  1. Tests in this package can plug in a mock Manager and exercise
//     handler.go without spinning up a real Server.
//  2. daemon.Server can satisfy it implicitly via its existing methods
//     (no wrapper struct needed).
//
// Every method here maps 1:1 to a model.Cmd* dispatcher case in the
// original Server.handleConn. The signatures mirror the original
// Server methods exactly so the implementation is a trivial passthrough.
//
// All methods are SAFE to call concurrently — the underlying
// ProcessRegistry holds the lock. ListAll returns a snapshot.
type Manager interface {
	// CmdStart
	StartApp(req *model.AppStartReq) ([]process.ProcessInfo, error)

	// CmdStop
	StopByName(name string) error

	// CmdRestart
	RestartByName(name string) error

	// CmdPause / CmdResume
	PauseByName(name string) error
	ResumeByName(name string) error

	// CmdDelete
	DeleteByName(name string) error

	// CmdList — returns a snapshot of every process's ProcessInfo
	ListAll() []process.ProcessInfo

	// CmdSave / CmdResurrect
	Save() error
	Resurrect() error

	// CmdKill — graceful stop of every managed process (does NOT exit
	// the daemon — handleConn's dispatcher schedules os.Exit separately).
	KillAll()

	// CmdPing — health check
	Ping()

	// CmdStatus — daemon identity + light runtime snapshot (PID,
	// started_at, version, home_dir, process_count)
	Status() process.DaemonInfo
}