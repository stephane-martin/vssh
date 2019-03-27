package lib

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/awnumar/memguard"
	gssh "github.com/stephane-martin/golang-ssh"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
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
	hkcb, err := MakeHostKeyCallback(sshParams.Insecure, l)
	if err != nil {
		return err
	}
	cfg.HostKey = hkcb

	var pre []string
	if len(env) != 0 {
		pre = append(pre, "env")
		pre = append(pre, EscapeEnv(env)...)
		if len(sshParams.Commands) == 0 {
			pre = append(pre, "bash")
		}
	}
	commands := append(pre, sshParams.Commands...)
	if len(sshParams.Commands) == 0 || sshParams.ForceTerminal {
		return gssh.Shell(ctx, cfg, os.Stdin, os.Stdout, os.Stderr, commands...)
	}
	err = gssh.OutputWithPty(ctx, cfg, strings.Join(commands, " "), os.Stdout, os.Stderr)
	if err != nil {
		return fmt.Errorf("failed to execute command: %s", err)
	}
	return nil
}
