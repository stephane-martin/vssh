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
	"github.com/mitchellh/go-homedir"
	gssh "github.com/stephane-martin/golang-ssh"
	vexec "github.com/stephane-martin/vault-exec/lib"
	"github.com/stephane-martin/vssh/lib"
	"github.com/urfave/cli"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
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

type Credentials struct {
	PrivateKey  *memguard.LockedBuffer
	PublicKey   *lib.PublicKey
	Certificate *memguard.LockedBuffer
}

func (c Credentials) AuthMethod() (ssh.AuthMethod, error) {
	if c.Certificate == nil {
		s, err := ssh.ParsePrivateKey(c.PrivateKey.Buffer())
		if err != nil {
			return nil, err
		}
		return ssh.PublicKeys(s), nil
	}
	ce, err := gssh.ParseCertificate(c.Certificate.Buffer())
	if err != nil {
		return nil, err
	}
	s, err := ssh.ParsePrivateKey(c.PrivateKey.Buffer())
	if err != nil {
		return nil, err
	}
	signer, err := ssh.NewCertSigner(ce, s)
	if err != nil {
		return nil, err
	}
	return ssh.PublicKeys(signer), nil
}

func getCredentials(ctx context.Context, c *cli.Context, loginName string, l *zap.SugaredLogger) (*api.Client, []Credentials, error) {
	privateKeyPath := c.String("privkey")
	if privateKeyPath == "" {
		p, err := homedir.Expand("~/.ssh/id_rsa")
		if err != nil {
			return nil, nil, err
		}
		privateKeyPath = p
	}

	var pubkeyFS *lib.PublicKey
	privkeyFS, err := lib.ReadPrivateKeyFromFileSystem(privateKeyPath)
	if err != nil {
		l.Infow("failed to read private key from filesystem", "path", privateKeyPath, "error", err)
	} else {
		pubkey, err := lib.DerivePublicKey(privkeyFS)
		if err != nil {
			l.Warnw("failed to derive public key from filesystem private key", "error", err)
		} else {
			pubkeyFS = pubkey
		}
	}
	var certificateFS *memguard.LockedBuffer
	if pubkeyFS != nil {
		certificatePath := privateKeyPath + "-cert.pub"
		_, err := os.Stat(certificatePath)
		if err == nil {
			cert, err := lib.ReadCertificateFromFileSystem(certificatePath)
			if err == nil {
				certificateFS = cert
			} else {
				l.Warnw("failed to read certificate from filesystem", "error", err)
			}
		} else {
			l.Infow("matching certificate not found for filesystem private key", "error", err)
		}
	}

	var vaultClient *api.Client
	vaultParams := getVaultParams(c)
	if vaultParams.SSHMount != "" {
		if vaultParams.SSHRole != "" {
			client, err := getVaultClient(ctx, vaultParams, l)
			if err == nil {
				vaultClient = client
			} else if err == context.Canceled {
				return nil, nil, err
			} else {
				l.Errorw("vault auth failed", "error", err)
			}

		} else {
			l.Infow("vault SSH role is not set")
		}
	} else {
		l.Infow("vault SSH mount point is not set")
	}

	privateKeyVaultPath := c.String("vprivkey")
	var privkeyVault *memguard.LockedBuffer
	if vaultClient != nil && privateKeyVaultPath != "" {
		pkey, err := lib.ReadPrivateKeyFromVault(ctx, privateKeyVaultPath, vaultClient, l)
		if err == nil {
			privkeyVault = pkey
		} else if err == context.Canceled {
			return nil, nil, err
		} else {
			l.Errorw("failed to read private key from vault", "error", err)
		}
	}
	var pubkeyVault *lib.PublicKey
	if privkeyVault != nil {
		pubkey, err := lib.DerivePublicKey(privkeyVault)
		if err != nil {
			l.Infow("failed to derive public key from vault private key", "error", err)
		} else {
			pubkeyVault = pubkey
		}
	}

	var certificatePKVault *memguard.LockedBuffer
	if pubkeyVault != nil && vaultClient != nil {
		signed, err := lib.Sign(ctx, pubkeyVault, loginName, vaultParams.SSHMount, vaultParams.SSHRole, vaultClient, l)
		if err == nil {
			certificatePKVault = signed
		} else if err == context.Canceled {
			return nil, nil, err
		} else {
			l.Errorw("failed to sign vault private key", "error", err)
		}
	}
	var certificatePKFS *memguard.LockedBuffer
	if certificatePKVault == nil && pubkeyFS != nil && vaultClient != nil {
		signed, err := lib.Sign(ctx, pubkeyFS, loginName, vaultParams.SSHMount, vaultParams.SSHRole, vaultClient, l)
		if err == nil {
			certificatePKFS = signed
		} else if err == context.Canceled {
			return nil, nil, err
		} else {
			l.Errorw("failed to sign filesystem private key", "error", err)
		}
	}

	var credentials []Credentials
	if certificatePKVault != nil {
		credentials = append(credentials, Credentials{
			PrivateKey:  privkeyVault,
			PublicKey:   pubkeyVault,
			Certificate: certificatePKVault,
		})
		l.Infow("enabled: private key from vault, signed by vault")
	}
	if certificatePKFS != nil {
		credentials = append(credentials, Credentials{
			PrivateKey:  privkeyFS,
			PublicKey:   pubkeyFS,
			Certificate: certificatePKFS,
		})
		l.Infow("enabled: private key from filesystem, signed by vault")
	}
	if pubkeyVault != nil {
		credentials = append(credentials, Credentials{
			PrivateKey: privkeyVault,
			PublicKey:  pubkeyVault,
		})
		l.Infow("enabled: private key from vault, no certificate")
	}
	if pubkeyFS != nil {
		if certificateFS != nil {
			credentials = append(credentials, Credentials{
				PrivateKey:  privkeyFS,
				PublicKey:   pubkeyFS,
				Certificate: certificateFS,
			})
			l.Infow("enabled: private key and certificate from filesystem")
		}
		credentials = append(credentials, Credentials{
			PrivateKey: privkeyFS,
			PublicKey:  pubkeyFS,
		})
		l.Infow("enabled private key from filesystem, no certificate")
	}
	return vaultClient, credentials, nil
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
