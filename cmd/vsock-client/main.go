package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"

	"golang.org/x/sys/unix"

	"github.com/lxc/lxd/shared/api"
	"github.com/lxc/lxd/shared/termios"
	"github.com/mdlayher/vsock"
	"github.com/pkg/errors"
)

var flagPort uint64
var flagContext uint64

func init() {
	flag.Uint64Var(&flagPort, "port", 8443, "Port to connect to")
	flag.Uint64Var(&flagContext, "context", 3, "Context ID")
}

var handlers = map[string]func(http.Client, string) (*http.Response, error){
	"/state": stateHandler,
	"/exec":  execHandler,
}

var args []string

func main() {
	flag.Usage = func() {
		fmt.Printf("Usage of %s:\n\n", os.Args[0])
		fmt.Printf("%s [options] method path\n\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()

	if len(flag.Args()) != 2 {
		flag.Usage()
		os.Exit(2)
	}

	client := http.Client{
		Transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return vsock.Dial(uint32(flagContext), uint32(flagPort))
			},
		},
	}

	// New HTTP request
	resp, err := handlers[flag.Arg(1)](client, flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}

	if resp == nil {
		return
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(string(body))
}

func stateHandler(client http.Client, method string) (*http.Response, error) {
	return client.Get("http://vsock/state")
}

func execHandler(client http.Client, method string) (*http.Response, error) {
	var err error

	// Set the environment
	env := map[string]string{}
	myTerm, ok := getTERM()
	if ok {
		env["TERM"] = myTerm
	}

	// Configure the terminal
	stdinFd := unix.Stdin
	stdoutFd := unix.Stdout

	stdinTerminal := termios.IsTerminal(stdinFd)
	stdoutTerminal := termios.IsTerminal(stdoutFd)

	// Determine interaction mode
	interactive := false

	// Record terminal state
	var oldttystate *termios.State
	if interactive && stdinTerminal {
		oldttystate, err = termios.MakeRaw(stdinFd)
		if err != nil {
			return nil, err
		}

		defer termios.Restore(stdinFd, oldttystate)
	}

	// Setup interactive console handler
	handler := controlSocketHandler
	if !interactive {
		handler = nil
	}

	// Grab current terminal dimensions
	var width, height int
	if stdoutTerminal {
		width, height, err = termios.GetSize(unix.Stdout)
		if err != nil {
			return nil, err
		}
	}

	stdin := os.Stdin
	stdout := os.Stdout

	// Prepare the command
	req := api.InstanceExecPost{
		Command:     []string{"ls", "-l", "/"},
		WaitForWS:   true,
		Interactive: interactive,
		Environment: env,
		Width:       width,
		Height:      height,
	}

	execArgs := InstanceExecArgs{
		Stdin:    stdin,
		Stdout:   stdout,
		Stderr:   os.Stderr,
		Control:  handler,
		DataDone: make(chan bool),
	}

	d := ProtocolLXD{
		http:     &client,
		httpHost: "http://vm.socket",
	}

	op, err := d.ExecInstance("", req, &execArgs)
	if err != nil {
		return nil, errors.Wrap(err, "ExecInstance")
	}

	// Wait for the operation to complete
	err = op.Wait()
	if err != nil {
		return nil, errors.Wrap(err, "op.Wait")
	}

	op.Get()

	// Wait for any remaining I/O to be flushed
	<-execArgs.DataDone

	return nil, nil
}
