package crypto

import (
	"context"
	"errors"
	"net"
	"os"

	"github.com/awnumar/memguard"
	"github.com/hashicorp/vault/api"
	"github.com/mitchellh/go-homedir"
	gssh "github.com/stephane-martin/golang-ssh"
	"github.com/stephane-martin/vssh/params"
	"github.com/stephane-martin/vssh/vault"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type SSHCredentials struct {
	PrivateKey  *memguard.LockedBuffer
	PublicKey   *PublicKey
	Certificate *memguard.LockedBuffer
	Password    *memguard.LockedBuffer
	Agent       bool
}

func (c SSHCredentials) AuthMethod() (ssh.AuthMethod, error) {
	if c.PrivateKey != nil && c.Certificate != nil {
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
	if c.PrivateKey != nil && c.Certificate == nil {
		s, err := ssh.ParsePrivateKey(c.PrivateKey.Buffer())
		if err != nil {
			return nil, err
		}
		return ssh.PublicKeys(s), nil
	}
	if c.Password != nil {
		return ssh.Password(string(c.Password.Buffer())), nil
	}
	if c.Agent {
		agent, err := GetAgentAuth()
		if err != nil {
			return nil, err
		}
		return agent, nil
	}
	return nil, errors.New("no credentials")
}

func GetAgentAuth() (ssh.AuthMethod, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if len(sock) == 0 {
		return nil, errors.New("SSH_AUTH_SOCK is not set")
	}
	agconn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, err
	}
	ag := agent.NewClient(agconn)
	auth := ssh.PublicKeysCallback(ag.Signers)
	return auth, nil
}

func CredentialsToMethods(credentials []SSHCredentials, logger *zap.SugaredLogger) (methods []ssh.AuthMethod) {
	for _, credential := range credentials {
		m, err := credential.AuthMethod()
		if err == nil {
			methods = append(methods, m)
		} else {
			logger.Errorw("failed to use credentials", "error", err)
		}
	}
	return methods
}

func GetSSHCredentials(ctx context.Context, clictx params.CLIContext, loginName string, useAgent bool, l *zap.SugaredLogger) (*api.Client, []SSHCredentials, error) {
	var credentials []SSHCredentials
	var vaultClient *api.Client

	vaultParams := vault.GetVaultParams(clictx)
	if vaultParams.SSHMount != "" {
		if vaultParams.SSHRole != "" {
			client, err := vault.GetVaultClient(ctx, vaultParams, l)
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

	if useAgent {
		l.Infow("enabled: auth by SSH agent")
		return vaultClient, []SSHCredentials{{Agent: true}}, nil
	}

	privateKeyPath := clictx.PrivateKey()
	if privateKeyPath == "" {
		privateKeyPath = "~/.ssh/id_rsa"
	}
	p, err := homedir.Expand(privateKeyPath)
	if err != nil {
		return nil, nil, err
	}
	privateKeyPath = p

	var pubkeyFS *PublicKey
	privkeyFS, err := ReadPrivateKeyFromFileSystem(privateKeyPath)
	if err != nil {
		l.Infow("failed to read private key from filesystem", "path", privateKeyPath, "error", err)
	} else {
		pubkey, err := DerivePublicKey(privkeyFS)
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
			cert, err := ReadCertificateFromFileSystem(certificatePath)
			if err == nil {
				certificateFS = cert
			} else {
				l.Warnw("failed to read certificate from filesystem", "error", err)
			}
		} else {
			l.Infow("matching certificate not found for filesystem private key", "error", err)
		}
	}

	privateKeyVaultPath := clictx.VPrivateKey()
	var privkeyVault *memguard.LockedBuffer
	if vaultClient != nil && privateKeyVaultPath != "" {
		pkey, err := vault.ReadPrivateKeyFromVault(ctx, privateKeyVaultPath, vaultClient, l)
		if err == nil {
			privkeyVault = pkey
		} else if err == context.Canceled {
			return nil, nil, err
		} else {
			l.Warnw("failed to read private key from vault", "error", err)
		}
	}
	var pubkeyVault *PublicKey
	if privkeyVault != nil {
		pubkey, err := DerivePublicKey(privkeyVault)
		if err != nil {
			l.Warnw("failed to derive public key from vault private key", "error", err)
		} else {
			pubkeyVault = pubkey
		}
	}

	var certificatePKVault *memguard.LockedBuffer
	if pubkeyVault != nil && vaultClient != nil {
		signed, err := Sign(ctx, pubkeyVault, loginName, vaultParams.SSHMount, vaultParams.SSHRole, vaultClient, l)
		if err == nil {
			certificatePKVault = signed
		} else if err == context.Canceled {
			return nil, nil, err
		} else {
			l.Warnw("failed to sign vault private key", "error", err)
		}
	}
	var certificatePKFS *memguard.LockedBuffer
	if certificatePKVault == nil && pubkeyFS != nil && vaultClient != nil {
		signed, err := Sign(ctx, pubkeyFS, loginName, vaultParams.SSHMount, vaultParams.SSHRole, vaultClient, l)
		if err == nil {
			certificatePKFS = signed
		} else if err == context.Canceled {
			return nil, nil, err
		} else {
			l.Warnw("failed to sign filesystem private key", "error", err)
		}
	}

	if certificatePKVault != nil {
		credentials = append(credentials, SSHCredentials{
			PrivateKey:  privkeyVault,
			PublicKey:   pubkeyVault,
			Certificate: certificatePKVault,
		})
		l.Infow("enabled: private key from vault, signed by vault")
	}
	if certificatePKFS != nil {
		credentials = append(credentials, SSHCredentials{
			PrivateKey:  privkeyFS,
			PublicKey:   pubkeyFS,
			Certificate: certificatePKFS,
		})
		l.Infow("enabled: private key from filesystem, signed by vault")
	}
	if pubkeyVault != nil {
		credentials = append(credentials, SSHCredentials{
			PrivateKey: privkeyVault,
			PublicKey:  pubkeyVault,
		})
		l.Infow("enabled: private key from vault, no certificate")
	}
	if pubkeyFS != nil {
		if certificateFS != nil {
			credentials = append(credentials, SSHCredentials{
				PrivateKey:  privkeyFS,
				PublicKey:   pubkeyFS,
				Certificate: certificateFS,
			})
			l.Infow("enabled: private key and certificate from filesystem")
		}

		credentials = append(credentials, SSHCredentials{
			PrivateKey: privkeyFS,
			PublicKey:  pubkeyFS,
		})
		l.Infow("enabled: private key from filesystem, no certificate")
	}
	if clictx.SSHPassword() {
		pass, err := InputPassword("Enter SSH password")
		if err != nil {
			return nil, nil, err
		}
		credentials = append(credentials, SSHCredentials{
			Password: pass,
		})
		l.Infow("enabled: SSH password")
	}
	return vaultClient, credentials, nil
}
