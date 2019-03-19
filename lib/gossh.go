package lib

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/mitchellh/go-homedir"
	gssh "github.com/stephane-martin/golang-ssh"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func GoSSH(sshParams SSHParams, privkeyPath, certPath string, env map[string]string, l *zap.SugaredLogger) error {
	auth, err := gssh.AuthCertFile(privkeyPath, certPath)
	if err != nil {
		return err
	}
	cfg := gssh.Config{
		User: sshParams.LoginName,
		Host: sshParams.Host,
		Port: sshParams.Port,
		Auth: []ssh.AuthMethod{auth},
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
		return client.Shell(commands...)
	}
	err = client.OutputWithPty(strings.Join(commands, " "), os.Stdout, os.Stderr)
	if err != nil {
		return fmt.Errorf("failed to execute command: %s", err)
	}
	return nil
}
