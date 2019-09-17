package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mdlayher/vsock"

	"github.com/monstermunchkin/vsock/shared"
)

var flagPort uint64

func init() {
	flag.Uint64Var(&flagPort, "port", 1234, "Port to listen on")
}

func main() {
	flag.Parse()

	r := mux.NewRouter()
	r.HandleFunc("/state", stateHandler)
	http.Handle("/", r)

	l, err := vsock.Listen(uint32(flagPort))
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()

	log.Fatal(http.Serve(l, nil))
}

func stateHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(shared.RenderState())
}
