package main

import (
	"os"
	"strconv"
)

type configuration struct {
	serverPort    int
	notifyThreads int
}

const (
	defaultPort          = 8080
	defaultNotifyThreads = 10
)

var cfg = configuration{}

func init() {
	readConfig()
}

func readConfig() {
	getEnvInt := func(key string, defaultValue int) int {
		if s, f := os.LookupEnv(key); f {
			if i, e := strconv.Atoi(s); e == nil {
				return i
			}
		}
		return defaultValue
	}
	cfg.serverPort = getEnvInt("HUB_PORT", defaultPort)
	cfg.notifyThreads = getEnvInt("HUB_NOTIFY_THREADS", defaultNotifyThreads)
}
