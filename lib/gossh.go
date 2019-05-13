package lib

import (
	"context"
	"errors"
	"fmt"
	"github.com/stephane-martin/vssh/params"
	"github.com/stephane-martin/vssh/sys"
	"os"
	"strings"

	"github.com/awnumar/memguard"
	gssh "github.com/stephane-martin/golang-ssh"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

func GoConnectAuth(ctx context.Context, sshParams params.SSHParams, terminal bool, auth []ssh.AuthMethod, env map[string]string, l *zap.SugaredLogger) error {
	if len(auth) == 0 {
		return errors.New("no auth method")
	}
	cfg := gssh.Config{
		User: sshParams.LoginName,
		Host: sshParams.Host,
		Port: sshParams.Port,
		Auth: auth,
	}
	hkcb, err := gssh.MakeHostKeyCallback(sshParams.Insecure, l)
	if err != nil {
		return err
	}
	cfg.HostKey = hkcb

	var pre []string
	if len(env) != 0 {
		pre = append(pre, "env")
		pre = append(pre, sys.EscapeEnv(env)...)
		if len(sshParams.Commands) == 0 {
			pre = append(pre, "bash")
		}
	}
	commands := append(pre, sshParams.Commands...)
	if len(sshParams.Commands) == 0 || terminal {
		return gssh.Shell(ctx, cfg, os.Stdin, os.Stdout, os.Stderr, commands...)
	}
	err = gssh.OutputWithPty(ctx, cfg, strings.Join(commands, " "), os.Stdout, os.Stderr)
	if err != nil {
		return fmt.Errorf("failed to execute command: %s", err)
	}
	return nil
}

func GoConnect(ctx context.Context, sshParams params.SSHParams, terminal bool, privkey, cert *memguard.LockedBuffer, env map[string]string, l *zap.SugaredLogger) error {
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
	return GoConnectAuth(ctx, sshParams, terminal, []ssh.AuthMethod{ssh.PublicKeys(signer)}, env, l)
}
