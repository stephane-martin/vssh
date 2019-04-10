package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"

	"github.com/awnumar/memguard"
	"github.com/hashicorp/vault/api"
	vexec "github.com/stephane-martin/vault-exec/lib"
	"github.com/stephane-martin/vssh/lib"
	"github.com/urfave/cli"
	"go.uber.org/zap"
)

func getVaultClient(ctx context.Context, vaultParams lib.VaultParams, l *zap.SugaredLogger) (*api.Client, error) {
	// unset env VAULT_ADDR to prevent the vault client from seeing it
	_ = os.Unsetenv("VAULT_ADDR")

	client, err := vexec.Auth(
		ctx,
		vaultParams.AuthMethod,
		vaultParams.Address,
		vaultParams.AuthPath,
		vaultParams.Token,
		vaultParams.Username,
		vaultParams.Password,
		l,
	)
	if err != nil {
		return nil, fmt.Errorf("Vault auth failed: %s", err)
	}
	err = vexec.CheckHealth(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("Vault health check error: %s", err)
	}
	return client, nil
}

func getVaultParams(c *cli.Context) lib.VaultParams {
	p := lib.VaultParams{
		SSHMount:   c.GlobalString("vault-ssh-mount"),
		SSHRole:    c.GlobalString("vault-ssh-role"),
		AuthMethod: strings.ToLower(c.GlobalString("vault-method")),
		AuthPath:   c.GlobalString("vault-auth-path"),
		Address:    c.GlobalString("vault-addr"),
		Token:      c.GlobalString("vault-token"),
		Username:   c.GlobalString("vault-username"),
		Password:   c.GlobalString("vault-password"),
	}
	if p.AuthMethod == "" {
		p.AuthMethod = "token"
	}
	if p.AuthPath == "" {
		p.AuthPath = p.AuthMethod
	}
	return p
}

func getCredentials(ctx context.Context, c *cli.Context, loginName string, l *zap.SugaredLogger) (*api.Client, *memguard.LockedBuffer, *memguard.LockedBuffer, *lib.PublicKey, error) {
	vaultParams := getVaultParams(c)
	if vaultParams.SSHMount == "" {
		return nil, nil, nil, nil, errors.New("empty SSH mount point")
	}
	if vaultParams.SSHRole == "" {
		return nil, nil, nil, nil, errors.New("empty SSH role")
	}

	client, err := getVaultClient(ctx, vaultParams, l)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("auth failed: %s", err)
	}

	privkey, err := lib.ReadPrivateKey(ctx, c.String("privkey"), c.String("vprivkey"), client, l)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to read private key: %s", err)
	}
	pubkey, err := lib.DerivePublicKey(privkey)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("error extracting public key: %s", err)
	}
	signed, err := lib.Sign(ctx, pubkey, loginName, vaultParams.SSHMount, vaultParams.SSHRole, client, l)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("signing error: %s", err)
	}
	return client, privkey, signed, pubkey, nil
}

func getSSHParams(c *cli.Context, verbose bool, args []string) (p lib.SSHParams, err error) {
	p.Verbose = verbose
	p.Host = strings.TrimSpace(args[0])
	if p.Host == "" {
		return p, errors.New("empty host")
	}
	spl := strings.SplitN(p.Host, "@", 2)
	if len(spl) == 2 {
		p.LoginName = spl[0]
		p.Host = spl[1]
	}
	if p.LoginName == "" {
		p.LoginName = c.String("login")
		if p.LoginName == "" {
			u, err := user.Current()
			if err != nil {
				return p, err
			}
			p.LoginName = u.Username
		}
	}
	p.Commands = args[1:]

	p.Insecure = c.Bool("insecure")
	p.Port = c.Int("ssh-port")
	p.Native = c.Bool("native")
	p.ForceTerminal = c.Bool("terminal")

	return p, nil
}
