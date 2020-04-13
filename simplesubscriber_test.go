package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	mathrand "math/rand"
	"net/http"
	"testing"
	"time"
)

const testTopic = "/a/topic"

var result string

func TestGet(t *testing.T) {
	subs := newSimpleSubscriber(0, testLogger)
	//Set up a few subscribers and topics..
	yesterday := time.Now().AddDate(0, 0, -1)
	today := time.Now()
	lease := time.Hour * 12
	subs.addSub("cb1", "topic1", "", yesterday, lease)
	subs.addSub("cb1", "topic2", "", today, lease)
	subs.addSub("cb2", "topic1", "", yesterday, lease)
	subs.addSub("cb3", "topic1", "", today, lease)
	subs.addSub("cb3", "topic2", "", today, lease)

	s := subs.get("nontopic")
	assert(t, s != nil, "Expected an empty array/slice, but were nil\n")
	assert(t, len(s) == 0, "Expected an empty array/slice, but got %v subscribers\n[%v]\n", len(s), s)

	s = subs.get("topic1")
	assert(t, len(s) == 1, "Expected one subscriber for topic1, but got %v\n[%v]\n", len(s), s)

	s = subs.get("topic2")
	assert(t, len(s) == 2, "Expected two subscribers for topic2, but got %v\n[%v]\n", len(s), s)
}

func TestNotify(t *testing.T) {
	b := make([]byte, 4)
	generateSecret := func() string {
		rand.Read(b)
		return hex.EncodeToString(b)
	}

	subscribers := []*struct {
		secret       string
		callbackPath string
	}{
		{secret: generateSecret()},
		{secret: generateSecret()},
		{secret: generateSecret()},
		{secret: generateSecret()},
		{secret: generateSecret()},
		{secret: generateSecret()},
		{secret: generateSecret()},
		{secret: generateSecret()},
		{secret: generateSecret()},
		{secret: generateSecret()},
	}
	// Create callback channel
	cbc := make(chan bool)
	// Setup subscriber callback handlers in the test server..
	callbacks := make(map[string]http.HandlerFunc)
	for i, s := range subscribers {
		s.callbackPath = fmt.Sprintf("/sub-%s", s.secret)
		callbacks[s.callbackPath] = func(i int, secret string) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				//t.Logf("Subscriber #%d Request validation started...\n", i)
				sign := r.Header.Get(headerXHubSignature)
				body, e := ioutil.ReadAll(r.Body)
				if e != nil {
					cbc <- false
					t.Logf("Subscriber #%d - Faild to read body, error: %v\n", i, e)
					return
				}
				sha := "sha512"
				hmac := hmac.New(sha512.New, []byte(secret))
				hmac.Write(body)
				computedSign := fmt.Sprintf("%s=%x", sha, hmac.Sum(nil))
				if sign != computedSign {
					cbc <- false
					t.Logf("Subscriber #%d - HMAC mismatch, received: %v, computed: %v\n", i, sign, computedSign)
					return
				}
				if r.Header.Get(headerLink) == "" {
					cbc <- false
					t.Logf("Subscriber #%d - Header Link missing/empty\n", i)
					return
				}
				cbc <- true
				//time.Sleep(time.Millisecond * 5)
				t.Logf("Subscriber #%d HTTP %v [%v] OK\n", i, r.Method, r.URL)
			}
		}(i, s.secret)
	}
	ts := startTestServer(t, callbacks)
	testURL := ts.URL
	t.Logf("Test server started on URL: %v", testURL)
	sub := newSimpleSubscriber(5, testLogger)
	s := startServer(sub)

	// Add subscribers
	today := time.Now()
	lease := time.Hour * 12
	for _, s := range subscribers {
		path := testURL + s.callbackPath
		sub.addSub(path, testTopic, s.secret, today, lease)
	}

	// Notify subscribers
	start := time.Now()
	sub.notify(testTopic, []byte("{\"testdata\": \"testvalue\"}"), "https://hub.example.com/", "/feed.json")

	// Wait for notifications..
	tot, fail := 0, 0
waitLoop:
	for {
		select {
		case r := <-cbc:
			{
				if !r {
					fail++
				}
				tot++
				if tot == len(subscribers) {
					break waitLoop
				}
			}
		case <-time.After(time.Millisecond * 100):
			t.Errorf("Timeout waiting for notifications, got %v out of %v", tot, len(subscribers))
		}
	}
	stop := time.Now()
	t.Logf("Notify time %v", stop.Sub(start))
	assert(t, tot == len(subscribers), "Got %v of %v notifications", tot, len(subscribers))
	assert(t, fail == 0, "%v subscribers faild to validate", fail)
	stopServers(s, ts)
	sub.stop()
}

func (s *simpleSubscriber) addSub(callback string, topic string, secret string, startTime time.Time, lease time.Duration) {
	sub := &subscription{topic: topic, callback: callback, secret: secret, started: startTime, leaseDuration: lease}
	t, ok := s.subscribers[topic]
	if !ok {
		s.subscribers[topic] = []*subscription{sub}
		return
	}
	s.subscribers[topic] = append(t, sub)
}

func benchmarkGenerateChallenge(b *testing.B, source func(p []byte) (n int, err error)) string {
	buf := make([]byte, 16)
	source(buf)
	return hex.EncodeToString(buf)
}

func BenchmarkGenerateChallengeCryptoRand(b *testing.B) {
	// Just a test to see slow/fast crypty/rand vs math/rand is, too see if it would be better to use math/rand instead of crypty/rand for generateChallenge..
	var r string
	for n := 0; n < b.N; n++ {
		r = benchmarkGenerateChallenge(b, rand.Read)
	}
	result = r // Store result to make sure that generateChallenge() is actually called and not removed by a compiler optimisation.
}

func BenchmarkGenerateChallengeMathRand(b *testing.B) {
	// Just a test to see slow/fast crypty/rand vs math/rand is, too see if it would be better to use math/rand instead of crypty/rand for generateChallenge..
	var r string
	for n := 0; n < b.N; n++ {
		r = benchmarkGenerateChallenge(b, mathrand.Read)
	}
	result = r // Store result to make sure that generateChallenge() is actually called and not removed by a compiler optimisation.
}
