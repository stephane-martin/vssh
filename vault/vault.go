package vault

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/awnumar/memguard"
	"github.com/stephane-martin/vssh/params"
	"os"
	"strings"

	"github.com/hashicorp/vault/api"
	vexec "github.com/stephane-martin/vault-exec/lib"
	"go.uber.org/zap"
)

func GetSecretsFromVault(ctx context.Context, client *api.Client, keys []string, prefix, upcase bool, l *zap.SugaredLogger) (map[string]string, error) {
	c := make(chan map[string]string, 1)
	var err error
	lCtx, lCancel := context.WithCancel(ctx)
	go func() {
		err = vexec.GetSecrets(
			lCtx,
			client,
			prefix, upcase, true,
			keys,
			l,
			c,
		)
		close(c)
	}()
	secrets := <-c
	lCancel()
	return secrets, err
}

func ReadPrivateKeyFromVault(ctx context.Context, vpath string, client *api.Client, l *zap.SugaredLogger) (*memguard.LockedBuffer, error) {
	m, err := GetSecretsFromVault(ctx, client, []string{vpath}, false, false, l)
	if err != nil {
		return nil, err
	}
	for _, v := range m {
		privkeyb := []byte(v)
		privkeyb2 := append(bytes.Trim(privkeyb, "\n"), '\n')
		privkey, err := memguard.NewImmutableFromBytes(privkeyb2)
		memguard.WipeBytes(privkeyb)
		if err != nil {
			return nil, err
		}
		return privkey, nil
	}
	return nil, errors.New("private key not found in Vault")
}

func GetVaultClient(ctx context.Context, vaultParams params.VaultParams, l *zap.SugaredLogger) (*api.Client, error) {
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

func GetVaultParams(c params.CLIContext) params.VaultParams {
	p := params.VaultParams{
		SSHMount:   c.VaultSSHMount(),
		SSHRole:    c.VaultSSHRole(),
		AuthMethod: strings.ToLower(c.VaultAuthMethod()),
		AuthPath:   c.VaultAuthPath(),
		Address:    c.VaultAddress(),
		Token:      c.VaultToken(),
		Username:   c.VaultUsername(),
		Password:   c.VaultPassword(),
	}
	if p.AuthMethod == "" {
		p.AuthMethod = "token"
	}
	if p.AuthPath == "" {
		p.AuthPath = p.AuthMethod
	}
	return p
}

