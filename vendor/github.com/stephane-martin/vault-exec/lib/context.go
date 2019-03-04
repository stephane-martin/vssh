package lib

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func GlobalContext() (context.Context, context.CancelFunc) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for range sigChan {
			cancel()
		}
	}()
	return ctx, cancel
}
