package lib

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/awnumar/memguard"
	"github.com/mitchellh/go-homedir"
	gssh "github.com/stephane-martin/golang-ssh"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func GoSSH(ctx context.Context, sshParams SSHParams, privkey, cert *memguard.LockedBuffer, env map[string]string, l *zap.SugaredLogger) error {
	c, err := gssh.ParseCertificate(cert.Buffer())
	if err != nil {
		return err
	}
	s, err := ssh.ParsePrivateKey(privkey.Buffer())
	if err != nil {
		return err
	}
	signer, err := ssh.NewCertSigner(c, s)
	if err != nil {
		return err
	}
	cfg := gssh.Config{
		User: sshParams.LoginName,
		Host: sshParams.Host,
		Port: sshParams.Port,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
	}
	if sshParams.Insecure {
		cfg.HostKey = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			l.Debugw("host key", "hostname", hostname, "remote", remote.String(), "key", string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(key))))
			return nil
		}
	} else {
		kh, err := homedir.Expand("~/.ssh/known_hosts")
		if err != nil {
			return fmt.Errorf("failed to expand known_hosts path: %s", err)
		}
		callback, err := knownhosts.New(kh)
		if err != nil {
			return fmt.Errorf("failed to open known_hosts file: %s", err)
		}
		cfg.HostKey = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			l.Debugw("host key", "hostname", hostname, "remote", remote.String(), "key", string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(key))))
			return callback(hostname, remote, key)
		}
	}
	var pre []string
	if len(env) != 0 {
		pre = append(pre, "env")
		pre = append(pre, EscapeEnv(env)...)
		if len(sshParams.Commands) == 0 {
			pre = append(pre, "bash")
		}
	}
	commands := append(pre, sshParams.Commands...)
	client := gssh.NewClient(cfg)
	if len(sshParams.Commands) == 0 || sshParams.ForceTerminal {
		return client.Shell(ctx, commands...)
	}
	err = client.OutputWithPty(ctx, strings.Join(commands, " "), os.Stdout, os.Stderr)
	if err != nil {
		return fmt.Errorf("failed to execute command: %s", err)
	}
	return nil
}
