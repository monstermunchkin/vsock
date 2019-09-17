package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/mdlayher/vsock"
)

var flagPort uint64
var flagContext uint64

func init() {
	flag.Uint64Var(&flagPort, "port", 1234, "Port to connect to")
	flag.Uint64Var(&flagContext, "context", 3, "Context ID")
}

func main() {
	flag.Usage = func() {
		fmt.Printf("Usage of %s:\n\n", os.Args[0])
		fmt.Printf("%s [options] path\n\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()

	if len(flag.Args()) != 1 {
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
	resp, err := client.Get(fmt.Sprintf("http://vsock%s", flag.Arg(0)))
	if err != nil {
		log.Fatal(err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(string(body))

}
