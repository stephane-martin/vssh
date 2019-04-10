package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func cancelOnSignal(cancel context.CancelFunc) {
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for range sigchan {
			cancel()
		}
	}()
}
