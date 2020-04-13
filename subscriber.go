package main

import (
	"time"
)

type (
	subscription struct {
		topic         string
		callback      string
		secret        string
		started       time.Time
		leaseDuration time.Duration
		leaseSeconds  int
	}
	subscriber interface {
		add(callback string, topic string, secret string, lease int)
		delete(callback string, topic string)
		notify(topic string, payload []byte, hubLink string, selfLink string)
	}
)
