package main

import (
	"net/http"
	"strconv"
	"sync"
)

// Single Request object
type Request struct {
	Token     string
	TokenSize int
	Model     string
	SLO       int
}

type RequestGroup struct {
	Requests []Request
	TokenSum int
	MinSLO   int
}

var (
	requestChannel = make(chan Request, 100)       // Channel to receive incoming requests
	modelGroups    = make(map[string]RequestGroup) // Map to store requests grouped by model
	processChannel = make(chan RequestGroup, 100)  // Channel to send preprocessed groups to sequential processor
	mu             sync.Mutex                      // Mutex to synchronize access to the modelGroups map
)

// parseRequest extracts data from an HTTP request and returns a Request object
func parseRequest(r *http.Request) Request {
	token := r.Header.Get("token")
	tokenSize := len(token)
	model := r.Header.Get("model")
	slo, _ := strconv.Atoi(r.Header.Get("slo"))

	return Request{
		Token:     token,
		TokenSize: tokenSize,
		Model:     model,
		SLO:       slo,
	}
}

// getOrCreateRequestGroup retrieves or creates a new RequestGroup for a given model
func getOrCreateRequestGroup(model string) RequestGroup {
	group, exists := modelGroups[model]
	if !exists {
		group = RequestGroup{}
	}
	return group
}

// addRequestToGroup adds a request to a request group and updates its properties
func addRequestToGroup(group *RequestGroup, req Request) {
	group.Requests = append(group.Requests, req)
	group.TokenSum += req.TokenSize
	if req.SLO < group.MinSLO || group.MinSLO == 0 {
		group.MinSLO = req.SLO
	}
}
