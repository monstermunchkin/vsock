package main

import (
	"io"

	"github.com/gorilla/websocket"
	"github.com/lxc/lxd/shared/api"
)

// The Operation type represents a currently running operation.
type Operation interface {
	Cancel() (err error)
	Get() (op api.Operation)
	GetWebsocket(secret string) (conn *websocket.Conn, err error)
	Refresh() (err error)
	Wait() (err error)
}

// The InstanceExecArgs struct is used to pass additional options during instance exec.
type InstanceExecArgs struct {
	// Standard input
	Stdin io.ReadCloser

	// Standard output
	Stdout io.WriteCloser

	// Standard error
	Stderr io.WriteCloser

	// Control message handler (window resize, signals, ...)
	Control func(conn *websocket.Conn)

	// Channel that will be closed when all data operations are done
	DataDone chan bool
}
