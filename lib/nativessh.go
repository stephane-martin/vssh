package lib

import (
	"bytes"
	"context"
	"fmt"
	"github.com/stephane-martin/vssh/crypto"
	"github.com/stephane-martin/vssh/params"
	"github.com/stephane-martin/vssh/sys"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/awnumar/memguard"
	"go.uber.org/zap"
)

func writePubkey(dir string, pub *crypto.PublicKey) (string, error) {
	pubkeyPath := filepath.Join(dir, "key.pub")
	serialized, err := crypto.SerializePublicKey(pub)
	if err != nil {
		return pubkeyPath, err
	}
	return pubkeyPath, writeKey(pubkeyPath, serialized)
}

func writePrivkey(dir string, priv *memguard.LockedBuffer) (string, error) {
	privkeyPath := filepath.Join(dir, "key")
	return privkeyPath, writeKey(privkeyPath, priv)
}

func writeCert(dir string, cert *memguard.LockedBuffer) (string, error) {
	certPath := filepath.Join(dir, "key-cert.pub")
	return certPath, writeKey(certPath, cert)
}

func writeKey(path string, key *memguard.LockedBuffer) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(bytes.Trim(key.Buffer(), "\n"))
	if err != nil {
		return err
	}
	_, err = f.WriteString("\n")
	return err
}

func NativeConnect(ctx context.Context, sshParams params.SSHParams, terminal, verbose bool, priv *memguard.LockedBuffer, pub *crypto.PublicKey, cert *memguard.LockedBuffer, env map[string]string, l *zap.SugaredLogger) error {
	dir, err := ioutil.TempDir("", "vssh")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %s", err)
	}
	l.Debugw("using temp directory", "dirname", dir)
	defer func() { _ = os.RemoveAll(dir) }()

	pubkeyPath, err := writePubkey(dir, pub)
	defer func() { _ = os.Remove(pubkeyPath) }()
	if err != nil {
		return err
	}
	privkeyPath, err := writePrivkey(dir, priv)
	defer func() { _ = os.Remove(privkeyPath) }()
	if err != nil {
		return err
	}
	certPath, err := writeCert(dir, cert)
	defer func() { _ = os.Remove(certPath) }()
	if err != nil {
		return err
	}

	var allArgs []string
	if verbose {
		allArgs = append(allArgs, "-v")
	}
	if terminal {
		allArgs = append(allArgs, "-t")
	}

	if sshParams.Insecure {
		allArgs = append(
			allArgs,
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
		)
	}
	allArgs = append(
		allArgs,
		"-o", fmt.Sprintf("IdentityFile=%s", privkeyPath),
		"-o", fmt.Sprintf("CertificateFile=%s", certPath),
		"-o", "IdentitiesOnly=yes",
		"-o", "IdentityAgent=none",
		"-o", "AddKeysToAgent=no",
		"-o", "ForwardAgent=no",
		"-l", strings.Replace(sshParams.LoginName, " ", `\ `, -1),
		"-p", fmt.Sprintf("%d", sshParams.Port),
		sshParams.Host,
	)
	if len(env) != 0 {
		var pre []string
		pre = append(pre, "env")
		pre = append(pre, sys.EscapeEnv(env)...)
		if len(sshParams.Commands) == 0 {
			pre = append([]string{"-t"}, pre...)
			pre = append(pre, "bash")
		}
		allArgs = append(allArgs, pre...)
	}
	allArgs = append(allArgs, sshParams.Commands...)

	cmd := exec.CommandContext(ctx, "ssh", allArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
