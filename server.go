package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type (
	httpServer struct {
		http.Server
		logger *log.Logger
		port   int
		router *http.ServeMux
		subscriber
	}
)

const (
	hubCallback     = "hub.callback"
	hubChallenge    = "hub.challenge"
	hubLeaseSeconds = "hub.lease_seconds"
	hubMode         = "hub.mode"
	hubSecret       = "hub.secret"
	hubTopic        = "hub.topic"
	modeSubscribe   = "subscribe"
	modeUnsubscribe = "unsubscribe"
)

func newServer(port int, logger *log.Logger, subscriber subscriber) *httpServer {
	svr := &httpServer{
		logger:     logger,
		port:       port,
		Server:     http.Server{Addr: fmt.Sprintf(":%d", port)},
		subscriber: subscriber,
		router:     http.NewServeMux(),
	}
	svr.Handler = svr.router
	svr.router.HandleFunc("/push", svr.logHTTP(svr.pushHandler))
	svr.router.HandleFunc("/", svr.logHTTP(svr.dumpRequest(svr.rootHandler)))
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		svr.logger.Printf("HTTP server starting on port: %d...\n", svr.port)
		defer svr.logger.Printf("HTTP server on port %d is stopped.\n", svr.port)
		wg.Done()
		svr.ListenAndServe()
	}()
	wg.Wait() // Wait for server thread to start..
	return svr
}

func (s *httpServer) dumpRequest(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d, e := httputil.DumpRequest(r, true); e == nil {
			s.logger.Printf("HTTP Request: %q\n", d)
		} else {
			s.logger.Printf("Failed to dump HTTP request, error: %v\nRequest: %v\n", e, r)
		}
		handler(w, r)
	}
}

func (s *httpServer) logHTTP(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.logger.Printf("HTTP: [%v] %v\n", r.Method, r.URL)
		handler(w, r)
	}
}

func (s *httpServer) pushHandler(w http.ResponseWriter, r *http.Request) {
	var payload []byte
	if r.Method == "POST" && r.Header.Get(headerContentType) == "application/json" {
		if data, e := ioutil.ReadAll(r.Body); e == nil {
			payload = data
		}
	}
	if payload == nil {
		payload = []byte("{\"data\": \"example payload\"}")
	}
	s.notify("/a/topic", payload, "https://hub.example.com/", "/feed.json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte{})
}

func (s *httpServer) rootHandler(w http.ResponseWriter, r *http.Request) {
	// All URL's that don't match a path in the mux, is sent to this handler.
	// Subscribe and unsubscribe events will be POST'ed to /
	if r.Method == "POST" && r.RequestURI == "/" {
		s.subscriptionHandler(w, r)
		return
	}

	s.logger.Printf("Ignoring call to %v\n", r.RequestURI)
	w.WriteHeader(http.StatusForbidden)
}

func (s *httpServer) subscriptionHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.logger.Printf("Failed to parse POST parameters: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	getParam := func(key string) (value string, exist bool) {
		value = r.Form.Get(key)
		_, exist = r.Form[key]
		return
	}
	isValidURL := func(callback string) bool {
		u, e := url.Parse(callback)
		return e == nil && u.Scheme != "" && u.Host != ""
	}
	callback, callbackOk := getParam(hubCallback)
	mode, modeOk := getParam(hubMode)
	topic, topicOk := getParam(hubTopic)
	secret, _ := getParam(hubSecret)
	leaseStr, _ := getParam(hubLeaseSeconds)
	lease, e := strconv.Atoi(leaseStr)
	if e != nil {
		lease = defaultLeaseSeconds
	}

	// Check that we have all the required fields
	requiredFields := callbackOk && modeOk && topicOk && len(topic) > 1
	if !requiredFields || !isValidURL(callback) {
		s.logger.Printf("Required field(s) missing or callback is not a valid URL! Subscriber prameters: %v\n", r.Form)

		w.WriteHeader(http.StatusBadRequest)
		// Return plain text errors in the body..
		w.Write([]byte("required field(s) missing or callback is not a valid URL\n"))
		return
	}

	if mode == modeSubscribe {
		// Handle subscribe and re-subscribe requests
		if s.subscriber != nil {
			s.subscriber.add(callback, topic, secret, lease)
		}
		// Skip the HTTP 202, that we shoud respond according to the WebSub spec, since the blackbox seems to think it's an error...
		//w.WriteHeader(http.StatusAccepted)
		return
	} else if mode == modeUnsubscribe {
		// Handle unsubscribe requests
		if s.subscriber != nil {
			s.subscriber.delete(callback, topic)
		}
		// Skip the HTTP 202, that we shoud respond according to the WebSub spec, since the blackbox seems to think it's an error...
		//w.WriteHeader(http.StatusAccepted)
		return
	}
	s.logger.Printf("Mode \"%v\" is not supported!\n", mode)
	w.WriteHeader(http.StatusBadRequest)
	// Return plain text errors in the body..
	w.Write([]byte("unsupported mode\n"))
}

func (s *httpServer) stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		s.logger.Printf("Failed to gracefully shutdown HTTP server, error: %v\n", err)
	}
}
