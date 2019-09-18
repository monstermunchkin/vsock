package main

import (
	"net/http"
	"net/url"

	"github.com/lxc/lxd/shared/logger"
)

// Return true if this an API request coming from a cluster node that is
// notifying us of some user-initiated API request that needs some action to be
// taken on this node as well.
func isClusterNotification(r *http.Request) bool {
	return r.Header.Get("User-Agent") == "lxd-cluster-notifier"
}

// Extract the project query parameter from the given request.
func projectParam(request *http.Request) string {
	project := queryParam(request, "project")
	if project == "" {
		project = "default"
	}
	return project
}

// Extract the given query parameter directly from the URL, never from an
// encoded body.
func queryParam(request *http.Request, key string) string {
	var values url.Values
	var err error

	if request.URL != nil {
		values, err = url.ParseQuery(request.URL.RawQuery)
		if err != nil {
			logger.Warnf("Failed to parse query string %q: %v", request.URL.RawQuery, err)
			return ""
		}
	}

	if values == nil {
		values = make(url.Values)
	}

	return values.Get(key)
}
