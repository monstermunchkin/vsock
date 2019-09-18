package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	neturl "net/url"

	"github.com/gorilla/websocket"
	"github.com/lxc/lxd/shared"
	"github.com/lxc/lxd/shared/api"
	"github.com/lxc/lxd/shared/logger"
	"gopkg.in/macaroon-bakery.v2/bakery"
	"gopkg.in/macaroon-bakery.v2/httpbakery"
)

// ProtocolLXD represents a LXD API server
type ProtocolLXD struct {
	server *api.Server

	http            *http.Client
	httpCertificate string
	httpHost        string
	httpUnixPath    string
	httpProtocol    string
	httpUserAgent   string

	bakeryClient         *httpbakery.Client
	bakeryInteractor     []httpbakery.Interactor
	requireAuthenticated bool

	clusterTarget string
	project       string
}

func (r *ProtocolLXD) setQueryAttributes(uri string) (string, error) {
	// Parse the full URI
	fields, err := neturl.Parse(uri)
	if err != nil {
		return "", err
	}

	// Extract query fields and update for cluster targeting or project
	values := fields.Query()
	if r.clusterTarget != "" {
		if values.Get("target") == "" {
			values.Set("target", r.clusterTarget)
		}
	}

	if r.project != "" {
		if values.Get("project") == "" {
			values.Set("project", r.project)
		}
	}
	fields.RawQuery = values.Encode()

	return fields.String(), nil
}

func (r *ProtocolLXD) query(method string, path string, data interface{}, ETag string) (*api.Response, string, error) {
	// Generate the URL
	url := fmt.Sprintf("%s/1.0%s", r.httpHost, path)

	// Add project/target
	url, err := r.setQueryAttributes(url)
	if err != nil {
		return nil, "", err
	}

	// Run the actual query
	return r.rawQuery(method, url, data, ETag)
}

func (r *ProtocolLXD) rawQuery(method string, url string, data interface{}, ETag string) (*api.Response, string, error) {
	var req *http.Request
	var err error

	// Log the request
	log.Println("Sending request to LXD",
		"method", method,
		"url", url,
		"etag", ETag,
	)

	// Get a new HTTP request setup
	if data != nil {
		switch data.(type) {
		case io.Reader:
			// Some data to be sent along with the request
			req, err = http.NewRequest(method, url, data.(io.Reader))
			if err != nil {
				return nil, "", err
			}

			// Set the encoding accordingly
			req.Header.Set("Content-Type", "application/octet-stream")
		default:
			// Encode the provided data
			buf := bytes.Buffer{}
			err := json.NewEncoder(&buf).Encode(data)
			if err != nil {
				return nil, "", err
			}

			// Some data to be sent along with the request
			// Use a reader since the request body needs to be seekable
			req, err = http.NewRequest(method, url, bytes.NewReader(buf.Bytes()))
			if err != nil {
				return nil, "", err
			}

			// Set the encoding accordingly
			req.Header.Set("Content-Type", "application/json")

			// Log the data
			log.Println(logger.Pretty(data))
		}
	} else {
		// No data to be sent along with the request
		req, err = http.NewRequest(method, url, nil)
		if err != nil {
			return nil, "", err
		}
	}

	// Set the user agent
	if r.httpUserAgent != "" {
		req.Header.Set("User-Agent", r.httpUserAgent)
	}

	// Set the ETag
	if ETag != "" {
		req.Header.Set("If-Match", ETag)
	}

	// Set the authentication header
	if r.requireAuthenticated {
		req.Header.Set("X-LXD-authenticated", "true")
	}

	// Send the request
	resp, err := r.do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	return lxdParseResponse(resp)
}

// Do performs a Request, using macaroon authentication if set.
func (r *ProtocolLXD) do(req *http.Request) (*http.Response, error) {
	if r.bakeryClient != nil {
		r.addMacaroonHeaders(req)
		return r.bakeryClient.Do(req)
	}

	return r.http.Do(req)
}

func (r *ProtocolLXD) addMacaroonHeaders(req *http.Request) {
	req.Header.Set(httpbakery.BakeryProtocolHeader, fmt.Sprint(bakery.LatestVersion))

	for _, cookie := range r.http.Jar.Cookies(req.URL) {
		req.AddCookie(cookie)
	}
}

func (r *ProtocolLXD) queryOperation(method string, path string, data interface{}, ETag string) (Operation, string, error) {
	// Send the query
	resp, etag, err := r.query(method, path, data, ETag)
	if err != nil {

		return nil, "", err
	}

	// Get to the operation
	respOperation, err := resp.MetadataAsOperation()
	if err != nil {
		return nil, "", err
	}

	// Setup an Operation wrapper
	op := operation{
		Operation: *respOperation,
		r:         r,
		chActive:  make(chan bool),
	}

	// Log the data
	log.Println("Got operation from LXD")
	log.Println(logger.Pretty(op.Operation))

	return &op, etag, nil
}

// ExecInstance requests that LXD spawns a command inside the instance.
func (r *ProtocolLXD) ExecInstance(instanceName string, exec api.InstanceExecPost, args *InstanceExecArgs) (Operation, error) {
	// Send the request
	op, _, err := r.queryOperation("POST", "/exec", exec, "")
	if err != nil {
		return nil, err
	}
	opAPI := op.Get()

	// Process additional arguments
	if args != nil {
		// Parse the fds
		fds := map[string]string{}

		value, ok := opAPI.Metadata["fds"]
		if ok {
			values := value.(map[string]interface{})
			for k, v := range values {
				fds[k] = v.(string)
			}
		}

		// Call the control handler with a connection to the control socket
		if args.Control != nil && fds["control"] != "" {
			conn, err := r.GetOperationWebsocket(opAPI.ID, fds["control"])
			if err != nil {
				return nil, err
			}

			go args.Control(conn)
		}

		if exec.Interactive {
			// Handle interactive sections
			if args.Stdin != nil && args.Stdout != nil {
				// Connect to the websocket
				conn, err := r.GetOperationWebsocket(opAPI.ID, fds["0"])
				if err != nil {
					return nil, err
				}

				// And attach stdin and stdout to it
				go func() {
					shared.WebsocketSendStream(conn, args.Stdin, -1)
					<-shared.WebsocketRecvStream(args.Stdout, conn)
					conn.Close()

					if args.DataDone != nil {
						close(args.DataDone)
					}
				}()
			} else {
				if args.DataDone != nil {
					close(args.DataDone)
				}
			}
		} else {
			// Handle non-interactive sessions
			dones := map[int]chan bool{}
			conns := []*websocket.Conn{}

			// Handle stdin
			if fds["0"] != "" {
				conn, err := r.GetOperationWebsocket(opAPI.ID, fds["0"])
				if err != nil {
					return nil, err
				}

				conns = append(conns, conn)
				dones[0] = shared.WebsocketSendStream(conn, args.Stdin, -1)
			}

			// Handle stdout
			if fds["1"] != "" {
				conn, err := r.GetOperationWebsocket(opAPI.ID, fds["1"])
				if err != nil {
					return nil, err
				}

				conns = append(conns, conn)
				dones[1] = shared.WebsocketRecvStream(args.Stdout, conn)
			}

			// Handle stderr
			if fds["2"] != "" {
				conn, err := r.GetOperationWebsocket(opAPI.ID, fds["2"])
				if err != nil {
					return nil, err
				}

				conns = append(conns, conn)
				dones[2] = shared.WebsocketRecvStream(args.Stderr, conn)
			}

			// Wait for everything to be done
			go func() {
				for i, chDone := range dones {
					// Skip stdin, dealing with it separately below
					if i == 0 {
						continue
					}

					<-chDone
				}

				if fds["0"] != "" {
					if args.Stdin != nil {
						args.Stdin.Close()
					}

					// Empty the stdin channel but don't block on it as
					// stdin may be stuck in Read()
					go func() {
						<-dones[0]
					}()
				}

				for _, conn := range conns {
					conn.Close()
				}

				if args.DataDone != nil {
					close(args.DataDone)
				}
			}()
		}
	}

	return op, nil
}
