package main

import (
	"bytes"
	"fmt"
	"net"
	"strings"

	"github.com/mitchellh/go-homedir"
	gssh "github.com/stephane-martin/golang-ssh"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func GoSSH(privkeyPath, certPath, ruser, rhost string, port int, args []string, verbose, insecure, force bool, l *zap.SugaredLogger) error {
	auth, err := gssh.AuthCertFile(privkeyPath, certPath)
	if err != nil {
		return err
	}
	cfg := gssh.Config{
		User: ruser,
		Host: rhost,
		Port: port,
		Auth: []ssh.AuthMethod{auth},
	}
	if insecure {
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
	client := gssh.NewClient(cfg)
	if len(args) == 0 || force {
		return client.Shell(args...)
	}
	output, err := client.OutputWithPty(strings.Join(args, " "))
	fmt.Println(output)
	if err != nil {
		return fmt.Errorf("failed to execute command: %s", err)
	}
	return nil
}
