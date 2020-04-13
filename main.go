package main

import (
	"log"
	"os"
	"os/signal"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)
	sub := newSimpleSubscriber(cfg.notifyThreads, logger)
	svr := newServer(cfg.serverPort, logger, sub)

	// Block until a interrupt or kill signal is received.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, os.Kill)
	logger.Printf("Starting shutdown on %v signal..", <-sig)

	svr.stop()
	sub.stop()
}
