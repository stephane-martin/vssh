package sys

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func CancelOnSignal(cancel context.CancelFunc) {
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for range sigchan {
			cancel()
		}
	}()
}
