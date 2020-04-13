package main

import (
	"os"
	"testing"
)

func Test_readConfig(t *testing.T) {
	tests := []struct {
		envPort  string
		envNt    string
		readPort int
		readNt   int
	}{
		{envPort: "8443", envNt: "100", readPort: 8443, readNt: 100},
		{envPort: ":8443", envNt: "flera", readPort: defaultPort, readNt: defaultNotifyThreads},
		{envPort: "", envNt: "", readPort: defaultPort, readNt: defaultNotifyThreads},
	}
	setEnv := func(key string, value string) {
		if value != "" {
			os.Setenv(key, value)
		} else {
			os.Unsetenv(key)
		}
	}
	for _, tt := range tests {
		t.Run(tt.envPort, func(t *testing.T) {
			// Setup env
			setEnv("HUB_PORT", tt.envPort)
			setEnv("HUB_NOTIFY_THREADS", tt.envNt)

			// Read config
			readConfig()

			// Verify
			assert(t, cfg.serverPort == tt.readPort, "HUB_PORT mismatch, got %v, expected", cfg.serverPort, tt.readPort)
			assert(t, cfg.notifyThreads == tt.readNt, "HUB_NOTIFY_THREADS mismatch, got %v, expected", cfg.notifyThreads, tt.readNt)
		})
	}
}
