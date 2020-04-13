package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type (
	notification struct {
		subscription *subscription
		payload      []byte
		hubLink      string
		selfLink     string
	}
	verification struct {
		subscription *subscription
		mode         string
	}
	simpleSubscriber struct {
		wg          sync.WaitGroup
		logger      *log.Logger
		subscribers map[string][]*subscription
		verifyChan  chan *verification
		notifyChan  chan *notification
	}
)

const (
	defaultLeaseSeconds = 3600
	headerContentType   = "Content-type"
	headerLink          = "Link"
	headerXHubSignature = "X-Hub-Signature"
)

func newSimpleSubscriber(notifiers int, logger *log.Logger) *simpleSubscriber {
	if notifiers < 1 {
		notifiers = 1
	}
	s := &simpleSubscriber{
		wg:          sync.WaitGroup{},
		logger:      logger,
		subscribers: make(map[string][]*subscription),
		verifyChan:  make(chan *verification, 10),
		notifyChan:  make(chan *notification, notifiers+1),
	}

	// Start one verify routine
	go func() {
		defer func() {
			s.wg.Done()
			s.logger.Println("Verify routine stopped.")
		}()
		s.logger.Println("Verify routine started...")
		for v := range s.verifyChan {
			s.verify(v)
		}
	}()
	s.wg.Add(1) // Add verify routine to wait group

	// Start notifier routine(s)
	for i := 0; i < notifiers; i++ {
		go func(id int) {
			defer func() {
				s.wg.Done()
				s.logger.Printf("Notify routine #%v stopped.", id)
			}()
			s.logger.Printf("Notify routine #%v started...\n", id)
			for n := range s.notifyChan {
				s.notifySubscriber(n)
			}
		}(i + 1)
	}
	s.wg.Add(notifiers) // Add notify routine(s) to wait group
	return s
}

func (s *subscription) isActive() bool {
	// Check if subscription is active or expired..
	return s.started.Add(s.leaseDuration).Sub(time.Now()) > 0
}

func (s *simpleSubscriber) add(callback string, topic string, secret string, lease int) {
	// Create a new verification and send it to the verify channel for further processing.
	// It's not until the request have been verified that we check if it's a new subscription or a re-subscribe.
	s.verifyChan <- &verification{
		mode: modeSubscribe,
		subscription: &subscription{
			callback:      callback,
			topic:         topic,
			secret:        secret,
			leaseDuration: time.Second * time.Duration(lease),
			leaseSeconds:  lease,
		},
	}
}

func (s *simpleSubscriber) delete(callback string, topic string) {
	// Create a new verification and send it to the verify channel for further processing.
	// It's not until we start the verified that we check if it's an active subscription or not.
	s.verifyChan <- &verification{
		mode:         modeUnsubscribe,
		subscription: &subscription{callback: callback, topic: topic},
	}
}

func (s *simpleSubscriber) notify(topic string, payload []byte, hubLink string, selfLink string) {
	// Get a list of active subscribers for this topic..
	subs := s.get(topic)
	if len(subs) == 0 {
		s.logger.Printf("No active subscriptions for topic %v\n", topic)
		return
	}
	// Since notifyList will block if it becomes full, queue notifications in another routine to avoid blocking the
	// notify() call while processing notifications..
	go func(subs []*subscription, payload []byte, hubLink string, selfLink string) {
		for _, sub := range subs {
			s.notifyChan <- &notification{subscription: sub, payload: payload, hubLink: hubLink, selfLink: selfLink}
		}
	}(subs, payload, hubLink, selfLink)
}

func (s *simpleSubscriber) get(topic string) []*subscription {
	active := make([]*subscription, 0)
	subs, ok := s.subscribers[topic]
	if !ok {
		s.logger.Printf("No subscriptions for topic %v", topic)
		return active
	}

	// Only return subscribers with an active subscription
	for _, sub := range subs {
		if sub.isActive() {
			active = append(active, sub)
		}
	}
	return active
}

func (s *simpleSubscriber) notifySubscriber(n *notification) {
	// Prepare the request with the content...
	req, err := http.NewRequest("POST", n.subscription.callback, bytes.NewReader(n.payload))
	if err != nil {
		s.logger.Printf("Faied to create notify request, error %v\n", err)
		return
	}

	// Add HTTP headers..

	// Content-type: application/json
	req.Header.Set(headerContentType, "application/json")
	// Link: <https://hub.example.com/>; rel="hub", </feed.json>; rel="self"
	req.Header.Set(headerLink, fmt.Sprintf("<%s>; rel=\"hub\", <%s>; rel=\"self\"", n.hubLink, n.selfLink))

	if n.subscription.secret != "" {
		// Maybe add option to switch to SHA1 for backwards compatibility with old subscribers?
		sha := "sha512"
		hmac := hmac.New(sha512.New, []byte(n.subscription.secret))
		hmac.Write(n.payload)
		// X-Hub-Signature: sha512=....
		req.Header.Set(headerXHubSignature, fmt.Sprintf("%s=%x", sha, hmac.Sum(nil)))
	}
	// Notify subscriber
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		s.logger.Printf("Failed to call subscriber %v, error %v", n.subscription.callback, err)
		// According to WebSub spec, we should re-queue message and try to send it again later in this scenario..
		return
	}
	defer res.Body.Close()
	if res.StatusCode < 400 {
		return
	}
	// According to WebSub spec, we should re-queue message and try to send it again later in this scenario..
}

func (s *simpleSubscriber) stop() {
	close(s.verifyChan)
	close(s.notifyChan)
	// Wait for all verify and notify GO routines to exit..
	s.wg.Wait()
}

func (s *simpleSubscriber) verify(v *verification) {
	u, e := url.Parse(v.subscription.callback)
	if e != nil {
		s.logger.Printf("Failed to parse callback URL, error: %v", e)
		return
	}

	// Just for testing...
	if v.subscription.topic == "deny" {
		s.denySubscription(u, v)
		return
	}

	// Verify intent of subscriber..
	if !s.verifyIntent(u, v) {
		return
	}

	// All good, set start time for subscription..
	v.subscription.started = time.Now()

	// Verification OK, update subscriptions..
	if v.mode == modeSubscribe {
		s.addSubscription(v.subscription)
	} else if v.mode == modeUnsubscribe {
		s.removeSubscription(v.subscription)
	}
}

func (s *simpleSubscriber) denySubscription(u *url.URL, v *verification) {
	// Append WebSub required fields to callback URL.
	qs := url.Values{}
	qs.Set(hubMode, "denied")
	qs.Set(hubTopic, v.subscription.topic)
	u.RawQuery += qs.Encode()
	r, e := http.Get(u.String())
	if e != nil {
		s.logger.Printf("Failed send verification request, error: %v\nURL: %v\n", e, u)
		return
	}
	defer r.Body.Close()
	if r.StatusCode > 299 {
		s.logger.Printf("Subscriber returned HTTP %v, verification failed!", r.Status)
	}
}

func (s *simpleSubscriber) verifyIntent(u *url.URL, v *verification) bool {
	challenge := generateChallenge()

	// Append WebSub required fields to callback URL.
	qs := url.Values{}
	qs.Set(hubMode, v.mode)
	qs.Set(hubTopic, v.subscription.topic)
	qs.Set(hubChallenge, challenge)
	qs.Set(hubLeaseSeconds, strconv.Itoa(v.subscription.leaseSeconds))
	u.RawQuery += qs.Encode()
	r, e := http.Get(u.String())
	if e != nil {
		s.logger.Printf("Failed send verification request, error: %v\nURL: %v\n", e, u)
		return false
	}
	defer r.Body.Close()
	// WebSub: Subscriber must respond with an HTTP success (2XX) code..
	if r.StatusCode > 299 {
		s.logger.Printf("Subscriber returned HTTP %v, verification failed!\n", r.Status)
		return false
	}
	// WebSub: ..with a response body equal to te hub.challenge parameter.
	d, e := ioutil.ReadAll(r.Body)
	if e != nil {
		s.logger.Printf("Failed to read the body for the subscriber verification response! Error: %v\n", e)
		return false
	}
	if challenge != string(d) {
		s.logger.Printf("The received challenge (%v) does not match the sent challenge (%v)!\n", string(d), challenge)
		return false
	}
	return true
}

func (s *simpleSubscriber) addSubscription(sub *subscription) {
	// Ignore re-subscribe scenarios, treat all subscribe requests as new subscriptions for now...
	// Currently there is only one GO routine modifying subscribers, if subscribers is going to be
	// changed from other threads, this must be changed, or atleast protected by locks..
	t, ok := s.subscribers[sub.topic]
	if !ok {
		s.subscribers[sub.topic] = []*subscription{sub}
		return
	}
	s.subscribers[sub.topic] = append(t, sub)
}

func (s *simpleSubscriber) removeSubscription(del *subscription) {
	t, ok := s.subscribers[del.topic]
	if !ok {
		return
	}
	// Search subscriptions for matching callback and topic in the unsubscribe request
	for i, sub := range t {
		if sub.callback == del.callback && sub.topic == del.topic {
			// Remove item from slice by overwriting item to be removed by the last value, then
			// truncate slice.
			// Currently there is only one GO routine modifying subscribers, if subscribers is
			// going to be changed from other threads, this must be changed, or atleast protected
			// by locks..
			l := len(t) - 1
			if i < l {
				t[i] = t[l]
			}
			s.subscribers[sub.topic] = t[:l]
			return
		}
	}
}

func generateChallenge() string {
	// Generate a new random challenge string..
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
