package lib

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"

	"github.com/awnumar/memguard"
	"github.com/mitchellh/go-homedir"
	gssh "github.com/stephane-martin/golang-ssh"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type SCPSource struct {
	Name        string
	Reader      io.Reader
	Size        int64
	Permissions os.FileMode
	CloseFunc   func() error
}

func (s *SCPSource) Close() error {
	if s.CloseFunc == nil {
		return nil
	}
	return s.CloseFunc()
}

func FileSource(filename string) (*SCPSource, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	infos, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return &SCPSource{
		Name:        path.Base(filename),
		Reader:      f,
		Size:        infos.Size(),
		Permissions: infos.Mode().Perm(),
		CloseFunc:   f.Close,
	}, nil
}

func GoSCP(ctx context.Context, source *SCPSource, remotePath string, sshParams SSHParams, privkey, cert *memguard.LockedBuffer, l *zap.SugaredLogger) error {
	lctx, cancel := context.WithCancel(ctx)
	defer cancel()
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

	client := gssh.NewClient(cfg)

	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		remotePath = "."
	}

	command := ""
	if remotePath == "-" {
		command = "scp -qt -- -"
	} else {
		command = fmt.Sprintf("scp -qt %s", EscapeString(remotePath))
	}

	stdin, stdout, stderr, err := client.Start(lctx, command)
	if err != nil {
		return err
	}
	go func() {
		_, _ = io.Copy(os.Stderr, stderr)
	}()

	sName := strings.Replace(source.Name, "\n", " ", -1)
	_, err = fmt.Fprintf(
		stdin,
		"C%04o %d %s\n",
		source.Permissions.Perm(), source.Size, sName,
	)
	if err != nil {
		return err
	}

	code, message, err := readResponse(stdout)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("scp status %d: %s", code, message)
	}

	_, err = io.Copy(stdin, source.Reader)
	if err != nil {
		return err
	}

	code, message, err = readResponse(stdout)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("scp status %d: %s", code, message)
	}

	_, err = fmt.Fprint(stdin, "\x00")
	if err != nil {
		return err
	}
	stdin.Close()

	code, message, err = readResponse(stdout)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("scp status %d: %s", code, message)
	}

	return client.Wait()
}

func readResponse(reader io.Reader) (code byte, message string, err error) {
	resp := make([]byte, 1)
	_, err = reader.Read(resp)
	if err != nil {
		return 0, "", err
	}
	code = resp[0]
	if code == 0 {
		return code, "", nil
	}
	for {
		_, err = reader.Read(resp)
		if err != nil {
			return code, message, err
		}
		if resp[0] == '\n' {
			break
		}
		message = message + string(resp)
	}
	return code, message, nil
}
