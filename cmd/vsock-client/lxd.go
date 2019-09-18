package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	neturl "net/url"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/lxc/lxd/shared/api"
	"github.com/lxc/lxd/shared/logger"
)

// Internal functions
func lxdParseResponse(resp *http.Response) (*api.Response, string, error) {
	// Get the ETag
	etag := resp.Header.Get("ETag")

	// Decode the response
	decoder := json.NewDecoder(resp.Body)
	response := api.Response{}

	err := decoder.Decode(&response)
	if err != nil {
		// Check the return value for a cleaner error
		if resp.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("Failed to fetch %s: %s", resp.Request.URL.String(), resp.Status)
		}

		return nil, "", err
	}

	// Handle errors
	if response.Type == api.ErrorResponse {
		return nil, "", fmt.Errorf(response.Error)
	}

	return &response, etag, nil
}

func (r *ProtocolLXD) queryStruct(method string, path string, data interface{}, ETag string, target interface{}) (string, error) {
	resp, etag, err := r.query(method, path, data, ETag)
	if err != nil {
		return "", err
	}

	err = resp.MetadataAsStruct(&target)
	if err != nil {
		return "", err
	}

	// Log the data
	log.Println("Got response struct from LXD")
	log.Println(logger.Pretty(target))

	return etag, nil
}

func (r *ProtocolLXD) websocket(path string) (*websocket.Conn, error) {
	// Generate the URL
	var url string
	if strings.HasPrefix(r.httpHost, "https://") {
		url = fmt.Sprintf("wss://%s/1.0%s", strings.TrimPrefix(r.httpHost, "https://"), path)
	} else {
		url = fmt.Sprintf("ws://%s/1.0%s", strings.TrimPrefix(r.httpHost, "http://"), path)
	}

	return r.rawWebsocket(url)
}

func (r *ProtocolLXD) rawWebsocket(url string) (*websocket.Conn, error) {
	// Grab the http transport handler
	httpTransport := r.http.Transport.(*http.Transport)

	// Setup a new websocket dialer based on it
	dialer := websocket.Dialer{
		NetDial:         httpTransport.Dial,
		TLSClientConfig: httpTransport.TLSClientConfig,
		Proxy:           httpTransport.Proxy,
	}

	// Set the user agent
	headers := http.Header{}
	if r.httpUserAgent != "" {
		headers.Set("User-Agent", r.httpUserAgent)
	}

	if r.requireAuthenticated {
		headers.Set("X-LXD-authenticated", "true")
	}

	// Set macaroon headers if needed
	if r.bakeryClient != nil {
		u, err := neturl.Parse(r.httpHost) // use the http url, not the ws one
		if err != nil {
			return nil, err
		}
		req := &http.Request{URL: u, Header: headers}
		r.addMacaroonHeaders(req)
	}

	// Establish the connection
	conn, _, err := dialer.Dial(url, headers)
	if err != nil {
		return nil, err
	}

	// Log the data
	log.Println("Connected to the websocket")

	return conn, err
}
