package lib

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/awnumar/memguard"
	"github.com/mitchellh/go-homedir"
	gssh "github.com/stephane-martin/golang-ssh"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type Callback func(isDir, endOfDir bool, name string, perms os.FileMode, mtime time.Time, atime time.Time, content io.Reader) error

func Download(ctx context.Context, srcs []string, params SSHParams, privkey, cert *memguard.LockedBuffer, cb Callback, l *zap.SugaredLogger) error {
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
		User: params.LoginName,
		Host: params.Host,
		Port: params.Port,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
	}
	if params.Insecure {
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

	for _, source := range srcs {
		err := receive(ctx, client, source, privkey, cert, cb, l)
		if err != nil {
			return err
		}
	}
	return nil
}

func receive(ctx context.Context, clt *gssh.Client, src string, privkey, cert *memguard.LockedBuffer, cb Callback, l *zap.SugaredLogger) error {
	lctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var p string
	if src == "-" {
		p = "-- -"
	} else {
		p = EscapeString(src)
	}
	opts := "-q -f -r -p"
	command := fmt.Sprintf("scp %s %s", opts, p)
	l.Debugw("remote command", "cmd", command)
	stdin, stdout, stderr, err := clt.Start(lctx, command)
	if err != nil {
		return err
	}
	go func() {
		_, _ = io.Copy(os.Stderr, bufio.NewReader(stderr))
	}()
	bstdout := bufio.NewReader(stdout)
	err = receiveOne(stdin, bstdout, src, "", cb, l)
	if err != nil {
		_ = stdin.Close()
		cancel()
		_ = clt.Wait()
		return err
	}
	_ = stdin.Close()
	return clt.Wait()
}

var ztime time.Time

func receiveOne(stdin io.WriteCloser, stdout *bufio.Reader, src, lPath string, cb Callback, l *zap.SugaredLogger) error {
	_ = ack(stdin)
	var mtime time.Time
	var atime time.Time
	for {
		line, err := stdout.ReadBytes('\n')
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			return errors.New("unexpected new line")
		}
		msg := line[0]
		line = bytes.TrimSpace(line[1:])
		if msg == 'E' {
			_ = ack(stdin)
			return nil
		}
		if msg == 'T' {
			splits := bytes.Fields(line)
			if len(splits) < 4 {
				return errors.New("bad timestamp")
			}
			mtimesec, err := strconv.ParseInt(string(splits[0]), 10, 64)
			if err != nil {
				return fmt.Errorf("bad timestamp: %s", err)
			}
			mtimemsec, err := strconv.ParseInt(string(splits[1]), 10, 64)
			if err != nil {
				return fmt.Errorf("bad timestamp: %s", err)
			}
			atimesec, err := strconv.ParseInt(string(splits[2]), 10, 64)
			if err != nil {
				return fmt.Errorf("bad timestamp: %s", err)
			}
			atimemsec, err := strconv.ParseInt(string(splits[3]), 10, 64)
			if err != nil {
				return fmt.Errorf("bad timestamp: %s", err)
			}
			mtime = time.Unix(mtimesec, mtimemsec*1000).UTC()
			atime = time.Unix(atimesec, atimemsec*1000).UTC()
			_ = ack(stdin)
			continue
		}
		if msg != 'D' && msg != 'C' {
			return fmt.Errorf("unexpected: %s", line)
		}
		splits := bytes.Fields(line)
		if len(splits) < 3 {
			return errors.New("invalid header line")
		}
		perms, err := strconv.ParseInt(string(splits[0]), 0, 32)
		if err != nil {
			return fmt.Errorf("bad permissions: %s", err)
		}
		size, err := strconv.ParseInt(string(splits[1]), 10, 64)
		if err != nil {
			return fmt.Errorf("bad size: %s", err)
		}
		target := string(bytes.Join(splits[2:], []byte(" ")))
		if msg == 'D' {
			dirPath := filepath.Join(lPath, target)
			err := cb(true, false, dirPath, os.FileMode(perms), mtime, atime, nil)
			if err != nil {
				return err
			}
			err = receiveOne(stdin, stdout, src, dirPath, cb, l)
			if err != nil {
				return err
			}
			err = cb(true, true, dirPath, os.FileMode(perms), mtime, atime, nil)
			if err != nil {
				return err
			}
			mtime = ztime
			atime = ztime
			continue
		}
		// if msg == 'C'
		_ = ack(stdin)
		lr := &io.LimitedReader{R: stdout, N: size}
		err = cb(false, false, filepath.Join(lPath, target), os.FileMode(perms), mtime, atime, lr)
		if err != nil {
			return err
		}
		io.Copy(ioutil.Discard, lr)
		mtime = ztime
		atime = ztime
		_, _, _ = readResponse(stdout)
		_ = ack(stdin)
	}
}

func ack(w io.Writer) error {
	_, err := fmt.Fprint(w, "\x00")
	return err
}
