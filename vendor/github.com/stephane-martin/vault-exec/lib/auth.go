package lib

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/hashicorp/vault/api"
	"github.com/mitchellh/go-homedir"
	"go.uber.org/zap"
)

func Auth(authType, address, path, tok, username, password string, logger *zap.SugaredLogger) (*api.Client, error) {
	config := api.DefaultConfig()
	config.Address = address
	client, err := api.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("error creating vault client: %s", err)
	}
	switch authType {
	case "token":
		logger.Info("token based authentication")
		if tok == "" {
			logger.Debug("token not found on command line or env")
			tokenPath, err := homedir.Expand("~/.vault-token")
			if err == nil {
				infos, err := os.Stat(tokenPath)
				if err == nil && infos.Mode().IsRegular() {
					content, err := ioutil.ReadFile(tokenPath)
					if err == nil {
						tok = string(content)
						logger.Infow("using token from file", "file", tokenPath)
					}
				} else {
					logger.Debug("unable to read file token")
				}
			} else {
				logger.Debugw("unable to expand ~/.vault-token", "error", err)
			}
			if tok == "" {
				t, err := Input("enter token: ", true)
				if err != nil {
					return nil, fmt.Errorf("error reading token: %s", err)
				}
				tok = string(t)
			}
			if tok == "" {
				return nil, errors.New("empty token")
			}
		}
		client.SetToken(tok)

	case "userpass", "ldap":
		if username == "" {
			u, err := Input("enter username: ", false)
			if err != nil {
				return nil, fmt.Errorf("error reading username: %s", err)
			}
			if len(u) == 0 {
				return nil, errors.New("empty username")
			}
			username = string(u)
		}
		if password == "" {
			p, err := Input("enter password: ", true)
			if err != nil {
				return nil, fmt.Errorf("error reading password: %s", err)
			}
			if len(p) == 0 {
				return nil, errors.New("empty password")
			}
			password = string(p)
		}
		path = fmt.Sprintf("auth/%s/login/%s", path, username)
		options := map[string]interface{}{
			"password": password,
		}
		secret, err := client.Logical().Write(path, options)
		if err != nil {
			return nil, fmt.Errorf("vault auth error: %s", err)
		}
		client.SetToken(secret.Auth.ClientToken)

	case "approle":
		if username == "" {
			r, err := Input("enter RoleID: ", false)
			if err != nil {
				return nil, fmt.Errorf("error reading RoleID: %s", err)
			}
			if len(r) == 0 {
				return nil, errors.New("empty RoleID")
			}
			username = string(r)
		}
		if password == "" {
			s, err := Input("enter SecretID: ", true)
			if err != nil {
				return nil, fmt.Errorf("error reading SecretID: %s", err)
			}
			if len(s) == 0 {
				return nil, errors.New("empty SecretID")
			}
			password = string(s)
		}
		path = fmt.Sprintf("auth/%s/login", path)
		options := map[string]interface{}{
			"role_id":   username,
			"secret_id": password,
		}
		secret, err := client.Logical().Write(path, options)
		if err != nil {
			return nil, fmt.Errorf("vault auth error: %s", err)
		}
		client.SetToken(secret.Auth.ClientToken)

	default:
		return nil, fmt.Errorf("unknown auth type: %s", authType)
	}
	return client, nil
}
