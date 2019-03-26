package lib

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/awnumar/memguard"
	"github.com/mitchellh/go-homedir"
	gssh "github.com/stephane-martin/golang-ssh"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type Source interface {
	IsSource()
	Close() error
}

type UploadFileSource struct {
	Name        string
	Reader      io.Reader
	Size        int64
	Permissions os.FileMode
	CloseFunc   func() error
}

func (s *UploadFileSource) IsSource() {}

func (s *UploadFileSource) Close() error {
	if s.CloseFunc == nil {
		return nil
	}
	return s.CloseFunc()
}

type UploadDirSource struct {
	Name string
}

func (s *UploadDirSource) IsSource()    {}
func (s *UploadDirSource) Close() error { return nil }

func hasDir(s []Source) bool {
	for _, src := range s {
		if _, ok := src.(*UploadDirSource); ok {
			return true
		}
	}
	return false
}

func MakeSource(filename string) (Source, error) {
	var err error
	filename, err = filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	infos, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if infos.IsDir() {
		_ = f.Close()
		return &UploadDirSource{Name: filename}, nil
	}
	if infos.Mode().IsRegular() {
		return &UploadFileSource{
			Name:        filepath.Base(filename),
			Reader:      f,
			Size:        infos.Size(),
			Permissions: infos.Mode().Perm(),
			CloseFunc:   f.Close,
		}, nil
	}
	return nil, fmt.Errorf("is not a regular file: %s", filename)
}

func Upload(ctx context.Context, sources []Source, remotePath string, sshParams SSHParams, privkey, cert *memguard.LockedBuffer, l *zap.SugaredLogger) error {
	lctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel() // close the SSH session
		for _, s := range sources {
			_ = s.Close()
		}
	}()
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

	opts := "-q -t"
	if hasDir(sources) {
		opts += " -r"
	}
	if len(sources) > 1 {
		opts += " -d"
	}
	var p string
	if remotePath == "-" {
		p = "-- -"
	} else {
		p = EscapeString(remotePath)
	}
	command := fmt.Sprintf("scp %s %s", opts, p)
	l.Debugw("remote command", "cmd", command)
	stdin, stdout, stderr, err := client.Start(lctx, command)
	if err != nil {
		return err
	}
	go func() {
		_, _ = io.Copy(os.Stderr, bufio.NewReader(stderr))
	}()
	bstdout := bufio.NewReader(stdout)

	for _, source := range sources {
		err := sendOne(source, stdin, bstdout, l)
		if err != nil {
			_ = stdin.Close()
			return err
		}
	}
	_ = stdin.Close()
	l.Debugw("waiting for remote process")
	return client.Wait()
}

func sendDir(dirname string, stdin io.WriteCloser, stdout *bufio.Reader, l *zap.SugaredLogger) error {
	d, err := os.Open(dirname)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	stats, err := d.Stat()
	if err != nil {
		return err
	}
	l.Debugw("uploading directory", "name", dirname)
	sName := strings.Replace(filepath.Base(dirname), "\n", "_", -1)
	if sName == "/" {
		sName = ""
	}

	headerLine := fmt.Sprintf(
		"D%04o %d %s\n",
		stats.Mode().Perm(), 0, sName,
	)
	l.Debugw("header line", "sent", headerLine)
	_, err = io.WriteString(stdin, headerLine)
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

	filenames, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, fname := range filenames {
		fname = filepath.Join(dirname, fname)
		s, err := MakeSource(fname)
		if err != nil {
			if os.IsNotExist(err) {
				l.Warnw("file does not exist", "filename", fname, "error", err)
				continue
			}
			if os.IsPermission(err) {
				l.Warnw("access denied", "filename", fname, "error", err)
				continue
			}
			return err
		}
		err = sendOne(s, stdin, stdout, l)
		if err != nil {
			return err
		}
	}

	_, err = io.WriteString(stdin, "E\n")
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
	return nil
}

func sendOne(src Source, stdin io.WriteCloser, stdout *bufio.Reader, l *zap.SugaredLogger) error {
	defer func() { _ = src.Close() }()
	if source, ok := src.(*UploadDirSource); ok {
		return sendDir(source.Name, stdin, stdout, l)
	}
	source := src.(*UploadFileSource)
	l.Debugw("uploading", "filename", source.Name, "size", source.Size)
	sName := strings.Replace(source.Name, "\n", "_", -1)

	headerLine := fmt.Sprintf(
		"C%04o %d %s\n",
		source.Permissions.Perm(), source.Size, sName,
	)
	l.Debugw("header line", "sent", headerLine)
	_, err := io.WriteString(stdin, headerLine)
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

	n, err := io.Copy(stdin, source.Reader)
	l.Debugw("uploaded", "bytes", n)
	if err != nil {
		return err
	}

	if err := ack(stdin); err != nil {
		return err
	}

	code, message, err = readResponse(stdout)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("scp status %d: %s", code, message)
	}
	return nil
}

func readResponse(reader *bufio.Reader) (byte, string, error) {
	code, err := reader.ReadByte()
	if err != nil {
		return 0, "", err
	}
	if code == 0 {
		return code, "", nil
	}
	message, err := reader.ReadBytes('\n')
	message = bytes.TrimRight(message, "\n")
	return code, string(message), err
}
