package lib

import (
	"context"
	"errors"

	"github.com/hashicorp/vault/api"
)

func CheckHealth(ctx context.Context, client *api.Client) error {
	c := make(chan error)
	go func() {
		health, err := client.Sys().Health()
		if err != nil {
			c <- err
			return
		}
		if !health.Initialized {
			c <- errors.New("vault is not initialized")
			return
		}
		if health.Sealed {
			c <- errors.New("vault is sealed")
			return
		}
		close(c)
	}()
	select {
	case err := <-c:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
