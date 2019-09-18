package main

import (
	"fmt"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/lxc/lxd/shared/api"
)

// The Operation type represents an ongoing LXD operation (asynchronous processing)
type operation struct {
	api.Operation

	r            *ProtocolLXD
	handlerReady bool
	handlerLock  sync.Mutex

	chActive chan bool
}

// Cancel will request that LXD cancels the operation (if supported)
func (op *operation) Cancel() error {
	return op.r.DeleteOperation(op.ID)
}

// Get returns the API operation struct
func (op *operation) Get() api.Operation {
	return op.Operation
}

// GetWebsocket returns a raw websocket connection from the operation
func (op *operation) GetWebsocket(secret string) (*websocket.Conn, error) {
	return op.r.GetOperationWebsocket(op.ID, secret)
}

// Refresh pulls the current version of the operation and updates the struct
func (op *operation) Refresh() error {
	// Get the current version of the operation
	newOp, _, err := op.r.GetOperation(op.ID)
	if err != nil {
		return err
	}

	// Update the operation struct
	op.Operation = *newOp

	return nil
}

// Wait lets you wait until the operation reaches a final state
func (op *operation) Wait() error {
	// Check if not done already
	if op.StatusCode.IsFinal() {
		if op.Err != "" {
			return fmt.Errorf(op.Err)
		}

		return nil
	}

	// Make sure we have a listener setup
	err := op.setupListener()
	if err != nil {
		return err
	}

	<-op.chActive

	// We're done, parse the result
	if op.Err != "" {
		return fmt.Errorf(op.Err)
	}

	return nil
}

func (op *operation) setupListener() error {
	// Make sure we're not racing with ourselves
	op.handlerLock.Lock()
	defer op.handlerLock.Unlock()

	// We already have a listener setup
	if op.handlerReady {
		return nil
	}
	op.handlerReady = true
	chReady := make(chan bool)

	// And do a manual refresh to avoid races
	err := op.Refresh()
	if err != nil {
		close(op.chActive)
		close(chReady)

		return err
	}

	// Check if not done already
	if op.StatusCode.IsFinal() {
		close(op.chActive)
		close(chReady)

		if op.Err != "" {
			return fmt.Errorf(op.Err)
		}

		return nil
	}

	// Start processing background updates
	close(chReady)

	return nil
}

// The remoteOperation type represents an ongoing LXD operation between two servers
type remoteOperation struct {
	targetOp Operation

	handlers []func(api.Operation)

	chDone chan bool
	chPost chan bool
	err    error
}

// CancelTarget attempts to cancel the target operation
func (op *remoteOperation) CancelTarget() error {
	if op.targetOp == nil {
		return fmt.Errorf("No associated target operation")
	}

	return op.targetOp.Cancel()
}

// GetTarget returns the target operation
func (op *remoteOperation) GetTarget() (*api.Operation, error) {
	if op.targetOp == nil {
		return nil, fmt.Errorf("No associated target operation")
	}

	opAPI := op.targetOp.Get()
	return &opAPI, nil
}

// Wait lets you wait until the operation reaches a final state
func (op *remoteOperation) Wait() error {
	<-op.chDone

	if op.chPost != nil {
		<-op.chPost
	}

	return op.err
}
