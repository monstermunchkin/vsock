package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"

	"github.com/mdlayher/vsock"
)

var flagPort uint64
var flagContext uint64

func init() {
	flag.Uint64Var(&flagPort, "port", 1234, "Port to connect to")
	flag.Uint64Var(&flagContext, "context", 3, "Context ID")
}

func main() {
	flag.Parse()

	client := http.Client{
		Transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return vsock.Dial(uint32(flagContext), uint32(flagPort))
			},
		},
	}

	// New HTTP request
	resp, err := client.Get("http://vsock/state")
	if err != nil {
		log.Fatal(err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(string(body))

}
