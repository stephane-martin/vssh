package lib

import (
	"errors"

	"github.com/hashicorp/vault/api"
)

func CheckHealth(client *api.Client) error {
	health, err := client.Sys().Health()
	if err != nil {
		return err
	}
	if !health.Initialized {
		return errors.New("vault is not initialized")
	}
	if health.Sealed {
		return errors.New("vault is sealed")
	}
	return nil
}
