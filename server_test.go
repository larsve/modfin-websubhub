package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

type (
	serverHandlers map[string]http.HandlerFunc
)

const (
	testServerCallbackURL = "http://localhost:8881/webhook"
	webhookPath           = "/webhook"
)

var (
	testLogger                     = log.New(os.Stdout, "WebSubHubTest: ", log.LstdFlags|log.Lmicroseconds)
	requestChan chan *http.Request = make(chan *http.Request)
)

func TestSubscriptionHandler(t *testing.T) {
	tests := []struct {
		method string
		params url.Values
		res    int
	}{
		{method: "POST", params: nil, res: http.StatusInternalServerError},
		{method: "POST", params: url.Values{"key": {"value"}}, res: http.StatusBadRequest},
		{method: "POST", params: subscribeFormParams("invalidURL", modeSubscribe, "topic", "secret", 0), res: http.StatusBadRequest},
		{method: "POST", params: subscribeFormParams(testServerCallbackURL, "invalidmode", "topic", "secret", 0), res: http.StatusBadRequest},
		//{method: "POST", params: subscribeFormParams(testServerCallbackURL, modeSubscribe, "topic", "secret", 0), res: http.StatusAccepted}, //  According to WebSub spec
		{method: "POST", params: subscribeFormParams(testServerCallbackURL, modeSubscribe, "topic", "secret", 0), res: http.StatusOK}, // According to Blackbox
		//{method: "POST", params: subscribeFormParams(testServerCallbackURL, modeUnsubscribe, "topic", "secret", 0), res: http.StatusAccepted}, // According to WebSub spec
		{method: "POST", params: subscribeFormParams(testServerCallbackURL, modeUnsubscribe, "topic", "secret", 0), res: http.StatusOK}, // According to Blackbox
	}
	s := httpServer{logger: testLogger}
	w := &dummyWriter{}
	for i, tc := range tests {
		t.Run(fmt.Sprintf("Test#%v", i), func(t *testing.T) {
			r := &http.Request{Method: tc.method, Header: http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}}
			if tc.params != nil {
				r.Body = ioutil.NopCloser(strings.NewReader(tc.params.Encode()))
			} else {
				r.Body = nil
			}
			w.statusCode = http.StatusOK // Set the default value, unless WriteHeader is called from the handler with a diferent value
			s.subscriptionHandler(w, r)
			assert(t, tc.res == w.statusCode, "Expected %v, but got %v", tc.res, w.statusCode)
		})
	}
}

func TestSubscriberActions(t *testing.T) {
	ts := startTestServer(t, nil)
	testURL := ts.URL + webhookPath
	t.Logf("Test server started on URL: %v", testURL)
	tests := []struct {
		test     string
		params   url.Values
		callback bool
		cbparams url.Values
	}{
		{test: "Subscribe", params: subscribeFormParams(testURL, modeSubscribe, "topic", "subsecret", 0), callback: true},
		{test: "Re-Subscribe", params: subscribeFormParams(testURL, modeSubscribe, "topic", "resubsecret", 0), callback: true},
		{test: "Unsubscribe", params: subscribeFormParams(testURL, modeUnsubscribe, "topic", "unsubsecret", 0), callback: true},
		{test: "DenySubscribe", params: subscribeFormParams(testURL, modeSubscribe, "deny", "subsecret", 0), callback: true},
	}
	sub := newSimpleSubscriber(1, testLogger)
	s := startServer(sub)
	for _, tc := range tests {
		t.Run(tc.test, func(t *testing.T) {
			res, err := http.PostForm("http://localhost:8880", tc.params)
			assert(t, err == nil, "Subscribe call failed: %v\n", err)
			//assert(t, res.StatusCode == 202, "Subscription call ended with HTTP %v, expected HTTP 202\n", res.Status) // According to WebSub spec
			assert(t, res.StatusCode == 200, "Subscription call ended with HTTP %v, expected HTTP 200\n", res.Status) // According to blackbox

			// Wait for callback (or timeout)..
			select {
			case r := <-requestChan:
				{
					assert(t, tc.callback, "Got a unexpected callback: %v", r)
					if err := r.ParseForm(); err != nil {
						t.Errorf("Failed to parse callback request parameters, error: %v\n", err)
					}
				}
			case <-time.After(time.Millisecond * 500):
				assert(t, !tc.callback, "No callback received")
			}
			t.Logf("%v call succeded: %v\n", tc.test, res.Status)
		})
	}
	stopServers(s, ts)
	sub.stop()
}

func assert(t testing.TB, condition bool, msg string, v ...interface{}) {
	if !condition {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s:%d: "+msg+"\n", append([]interface{}{filepath.Base(file), line}, v...)...)
	}
}

func defaultWebhook(w http.ResponseWriter, r *http.Request) {
	requestChan <- r
	w.Write([]byte(r.URL.Query().Get(hubChallenge)))
}

type dummyWriter struct {
	header     http.Header
	statusCode int
}

func (d *dummyWriter) Header() http.Header        { return d.header }
func (d *dummyWriter) Write([]byte) (int, error)  { return 0, nil }
func (d *dummyWriter) WriteHeader(statusCode int) { d.statusCode = statusCode }

func startServer(subscribers subscriber) *httpServer {
	return newServer(8880, testLogger, subscribers)
}

func startTestServer(t *testing.T, h serverHandlers) *httptest.Server {
	mux := http.NewServeMux()
	for p, f := range h {
		mux.HandleFunc(p, f)
	}

	// Add webhook handler unless one already were provided..
	if _, ok := h[webhookPath]; !ok {
		mux.HandleFunc(webhookPath, defaultWebhook)
	}
	return httptest.NewServer(mux)
}

func stopServers(s *httpServer, t *httptest.Server) {
	s.stop()
	t.Close()
}

func subscribeFormParams(callback string, mode string, topic string, secret string, lease int) url.Values {
	// Set required parameters..
	p := url.Values{
		hubCallback: {callback},
		hubMode:     {mode},
		hubTopic:    {topic},
	}
	// Optional parameters..
	if lease > 0 {
		p.Set(hubLeaseSeconds, strconv.Itoa(lease))
	}
	if secret != "" {
		p.Set(hubSecret, secret)
	}
	return p
}
