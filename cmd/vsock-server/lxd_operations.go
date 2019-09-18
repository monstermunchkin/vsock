package main

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pborman/uuid"

	"github.com/lxc/lxd/shared"
	"github.com/lxc/lxd/shared/api"
	"github.com/lxc/lxd/shared/cancel"
	"github.com/lxc/lxd/shared/version"
)

var operationsLock sync.Mutex
var operations map[string]*operation = make(map[string]*operation)

type operationClass int

const (
	operationClassTask      operationClass = 1
	operationClassWebsocket operationClass = 2
	operationClassToken     operationClass = 3
)

func (t operationClass) String() string {
	return map[operationClass]string{
		operationClassTask:      "task",
		operationClassWebsocket: "websocket",
		operationClassToken:     "token",
	}[t]
}

type operation struct {
	project     string
	id          string
	class       operationClass
	createdAt   time.Time
	updatedAt   time.Time
	status      api.StatusCode
	url         string
	resources   map[string][]string
	metadata    map[string]interface{}
	err         string
	readonly    bool
	canceler    *cancel.Canceler
	description string
	permission  string

	// Those functions are called at various points in the Operation lifecycle
	onRun     func(*operation) error
	onCancel  func(*operation) error
	onConnect func(*operation, *http.Request, http.ResponseWriter) error

	// Channels used for error reporting and state tracking of background actions
	chanDone chan error

	// Locking for concurent access to the Operation
	lock sync.Mutex
}

func (op *operation) done() {
	if op.readonly {
		return
	}

	op.lock.Lock()
	op.readonly = true
	op.onRun = nil
	op.onCancel = nil
	op.onConnect = nil
	close(op.chanDone)
	op.lock.Unlock()

	time.AfterFunc(time.Second*5, func() {
		operationsLock.Lock()
		_, ok := operations[op.id]
		if !ok {
			operationsLock.Unlock()
			return
		}

		delete(operations, op.id)
		operationsLock.Unlock()
	})
}

func (op *operation) Run() (chan error, error) {
	if op.status != api.Pending {
		return nil, fmt.Errorf("Only pending operations can be started")
	}

	chanRun := make(chan error, 1)

	op.lock.Lock()
	op.status = api.Running

	if op.onRun != nil {
		go func(op *operation, chanRun chan error) {
			err := op.onRun(op)
			if err != nil {
				op.lock.Lock()
				op.status = api.Failure
				op.err = SmartError(err).String()
				op.lock.Unlock()
				op.done()
				chanRun <- err

				log.Printf("Failure for %s Operation: %s: %s\n", op.class.String(), op.id, err)

				op.Render()
				return
			}

			op.lock.Lock()
			op.status = api.Success
			op.lock.Unlock()
			op.done()
			chanRun <- nil

			op.lock.Lock()
			log.Printf("Success for %s Operation: %s\n", op.class.String(), op.id)
			op.Render()
			op.lock.Unlock()
		}(op, chanRun)
	}
	op.lock.Unlock()

	log.Printf("Started %s Operation: %s\n", op.class.String(), op.id)
	op.Render()

	return chanRun, nil
}

func (op *operation) Cancel() (chan error, error) {
	if op.status != api.Running {
		return nil, fmt.Errorf("Only running operations can be cancelled")
	}

	if !op.mayCancel() {
		return nil, fmt.Errorf("This Operation can't be cancelled")
	}

	chanCancel := make(chan error, 1)

	op.lock.Lock()
	oldStatus := op.status
	op.status = api.Cancelling
	op.lock.Unlock()

	if op.onCancel != nil {
		go func(op *operation, oldStatus api.StatusCode, chanCancel chan error) {
			err := op.onCancel(op)
			if err != nil {
				op.lock.Lock()
				op.status = oldStatus
				op.lock.Unlock()
				chanCancel <- err

				log.Printf("Failed to cancel %s Operation: %s: %s\n", op.class.String(), op.id, err)
				op.Render()
				return
			}

			op.lock.Lock()
			op.status = api.Cancelled
			op.lock.Unlock()
			op.done()
			chanCancel <- nil

			log.Printf("Cancelled %s Operation: %s\n", op.class.String(), op.id)
			op.Render()
		}(op, oldStatus, chanCancel)
	}

	log.Printf("Cancelling %s Operation: %s\n", op.class.String(), op.id)
	op.Render()

	if op.canceler != nil {
		err := op.canceler.Cancel()
		if err != nil {
			return nil, err
		}
	}

	if op.onCancel == nil {
		op.lock.Lock()
		op.status = api.Cancelled
		op.lock.Unlock()
		op.done()
		chanCancel <- nil
	}

	log.Printf("Cancelled %s Operation: %s\n", op.class.String(), op.id)
	op.Render()

	return chanCancel, nil
}

func (op *operation) Connect(r *http.Request, w http.ResponseWriter) (chan error, error) {
	if op.class != operationClassWebsocket {
		return nil, fmt.Errorf("Only websocket operations can be connected")
	}

	if op.status != api.Running {
		return nil, fmt.Errorf("Only running operations can be connected")
	}

	chanConnect := make(chan error, 1)

	op.lock.Lock()

	go func(op *operation, chanConnect chan error) {
		err := op.onConnect(op, r, w)
		if err != nil {
			chanConnect <- err

			log.Printf("Failed to handle %s Operation: %s: %s\n", op.class.String(), op.id, err)
			return
		}

		chanConnect <- nil

		log.Printf("Handled %s Operation: %s\n", op.class.String(), op.id)
	}(op, chanConnect)
	op.lock.Unlock()

	log.Printf("Connected %s Operation: %s\n", op.class.String(), op.id)
	return chanConnect, nil
}

func (op *operation) mayCancel() bool {
	if op.class == operationClassToken {
		return true
	}

	if op.onCancel != nil {
		return true
	}

	if op.canceler != nil && op.canceler.Cancelable() {
		return true
	}

	return false
}

func (op *operation) Render() (string, *api.Operation, error) {

	// Setup the resource URLs
	resources := op.resources
	if resources != nil {
		tmpResources := make(map[string][]string)
		for key, value := range resources {
			var values []string
			for _, c := range value {
				values = append(values, fmt.Sprintf("/%s/%s/%s", version.APIVersion, key, c))
			}
			tmpResources[key] = values
		}
		resources = tmpResources
	}

	return op.url, &api.Operation{
		ID:          op.id,
		Class:       op.class.String(),
		Description: op.description,
		CreatedAt:   op.createdAt,
		UpdatedAt:   op.updatedAt,
		Status:      op.status.String(),
		StatusCode:  op.status,
		Resources:   resources,
		Metadata:    op.metadata,
		MayCancel:   op.mayCancel(),
		Err:         op.err,
	}, nil
}

func (op *operation) WaitFinal(timeout int) (bool, error) {
	// Check current state
	if op.status.IsFinal() {
		return true, nil
	}

	// Wait indefinitely
	if timeout == -1 {
		<-op.chanDone
		return true, nil
	}

	// Wait until timeout
	if timeout > 0 {
		timer := time.NewTimer(time.Duration(timeout) * time.Second)
		select {
		case <-op.chanDone:
			return true, nil

		case <-timer.C:
			return false, nil
		}
	}

	return false, nil
}

func (op *operation) UpdateResources(opResources map[string][]string) error {
	if op.status != api.Pending && op.status != api.Running {
		return fmt.Errorf("Only pending or running operations can be updated")
	}

	if op.readonly {
		return fmt.Errorf("Read-only operations can't be updated")
	}

	op.lock.Lock()
	op.updatedAt = time.Now()
	op.resources = opResources
	op.lock.Unlock()

	log.Printf("Updated resources for %s Operation: %s\n", op.class.String(), op.id)
	op.Render()

	return nil
}

func (op *operation) UpdateMetadata(opMetadata interface{}) error {
	if op.status != api.Pending && op.status != api.Running {
		return fmt.Errorf("Only pending or running operations can be updated")
	}

	if op.readonly {
		return fmt.Errorf("Read-only operations can't be updated")
	}

	newMetadata, err := shared.ParseMetadata(opMetadata)
	if err != nil {
		return err
	}

	op.lock.Lock()
	op.updatedAt = time.Now()
	op.metadata = newMetadata
	op.lock.Unlock()

	log.Printf("Updated metadata for %s Operation: %s\n", op.class.String(), op.id)
	op.Render()

	return nil
}

func operationCreate(project string, opClass operationClass, opResources map[string][]string, opMetadata interface{}, onRun func(*operation) error, onCancel func(*operation) error, onConnect func(*operation, *http.Request, http.ResponseWriter) error) (*operation, error) {
	// Main attributes
	op := operation{}
	op.project = project
	op.id = uuid.NewRandom().String()
	// op.description = opType.Description()
	// op.permission = opType.Permission()
	op.class = opClass
	op.createdAt = time.Now()
	op.updatedAt = op.createdAt
	op.status = api.Pending
	op.url = fmt.Sprintf("/%s/operations/%s", version.APIVersion, op.id)
	op.resources = opResources
	op.chanDone = make(chan error)

	newMetadata, err := shared.ParseMetadata(opMetadata)
	if err != nil {
		return nil, err
	}
	op.metadata = newMetadata

	// Callback functions
	op.onRun = onRun
	op.onCancel = onCancel
	op.onConnect = onConnect

	// Sanity check
	if op.class != operationClassWebsocket && op.onConnect != nil {
		return nil, fmt.Errorf("Only websocket operations can have a Connect hook")
	}

	if op.class == operationClassWebsocket && op.onConnect == nil {
		return nil, fmt.Errorf("Websocket operations must have a Connect hook")
	}

	if op.class == operationClassToken && op.onRun != nil {
		return nil, fmt.Errorf("Token operations can't have a Run hook")
	}

	if op.class == operationClassToken && op.onCancel != nil {
		return nil, fmt.Errorf("Token operations can't have a Cancel hook")
	}

	operationsLock.Lock()
	operations[op.id] = &op
	operationsLock.Unlock()

	log.Printf("New %s Operation: %s\n", op.class.String(), op.id)
	op.Render()

	return &op, nil
}

func operationGetInternal(id string) (*operation, error) {
	operationsLock.Lock()
	op, ok := operations[id]
	operationsLock.Unlock()

	if !ok {
		return nil, fmt.Errorf("Operation '%s' doesn't exist", id)
	}

	return op, nil
}

type OperationWebSocket struct {
	Req *http.Request
	Op  *operation
}

func (r *OperationWebSocket) Render(w http.ResponseWriter) error {
	chanErr, err := r.Op.Connect(r.Req, w)
	if err != nil {
		return err
	}

	err = <-chanErr
	return err
}

func (r *OperationWebSocket) String() string {
	_, md, err := r.Op.Render()
	if err != nil {
		return fmt.Sprintf("error: %s", err)
	}

	return md.ID
}

type forwardedOperationWebSocket struct {
	req    *http.Request
	id     string
	source *websocket.Conn // Connection to the node were the Operation is running
}

func (r *forwardedOperationWebSocket) Render(w http.ResponseWriter) error {
	target, err := shared.WebsocketUpgrader.Upgrade(w, r.req, nil)
	if err != nil {
		return err
	}
	<-shared.WebsocketProxy(r.source, target)
	return nil
}

func (r *forwardedOperationWebSocket) String() string {
	return r.id
}
