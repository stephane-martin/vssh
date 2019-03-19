package lib

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

func Connect(sshParams SSHParams, priv []byte, pub, signed string, env map[string]string, l *zap.SugaredLogger) error {
	dir, err := ioutil.TempDir("", "vssh")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %s", err)
	}
	l.Debugw("using temp directory", "dirname", dir)
	defer os.RemoveAll(dir)
	pubkeyPath := filepath.Join(dir, "key.pub")
	privkeyPath := filepath.Join(dir, "key")
	certPath := filepath.Join(dir, "key-cert.pub")
	err = ioutil.WriteFile(pubkeyPath, append([]byte(pub), '\n'), 0600)
	if err != nil {
		return err
	}
	// TODO: remove temp files as soon as possible
	defer os.Remove(pubkeyPath)
	err = ioutil.WriteFile(privkeyPath, append(priv, '\n'), 0600)
	if err != nil {
		return err
	}
	defer os.Remove(privkeyPath)
	err = ioutil.WriteFile(certPath, []byte(signed), 0600)
	if err != nil {
		return err
	}
	defer os.Remove(certPath)
	if sshParams.Native {
		l.Debugw("native SSH client")
		return Native(sshParams, privkeyPath, certPath, env, l)
	}
	l.Debugw("builtin SSH client")
	return GoSSH(sshParams, privkeyPath, certPath, env, l)
}
