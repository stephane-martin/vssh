package lib

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/awnumar/memguard"
	gssh "github.com/stephane-martin/golang-ssh"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

// Callback is a function type that is used by ScpGet to return the remote SSH directories and files.
type Callback func(isDir, endOfDir bool, name string, perms os.FileMode, mtime, atime time.Time, content io.Reader) error

func SFTPListAuth(ctx context.Context, params SSHParams, auth []ssh.AuthMethod, l *zap.SugaredLogger, cb ListCallback) error {
	if len(auth) == 0 {
		return errors.New("no auth method")
	}
	cfg := gssh.Config{
		User: params.LoginName,
		Host: params.Host,
		Port: params.Port,
		Auth: auth,
	}
	hkcb, err := gssh.MakeHostKeyCallback(params.Insecure, l)
	if err != nil {
		return err
	}
	cfg.HostKey = hkcb
	client, err := gssh.SFTP(cfg)
	if err != nil {
		return err
	}

	stopping := make(chan struct{})
	defer close(stopping)
	go func() {
		select {
		case <-ctx.Done():
		case <-stopping:
		}
		_ = client.Close()
	}()

	wd, err := client.Getwd()
	if err != nil {
		return err
	}
	walker := client.Walk(".")
	for walker.Step() {
		path := walker.Path()
		infos := walker.Stat()
		if walker.Err() == nil && path != "." && (infos.IsDir() || infos.Mode().IsRegular()) {
			err := cb(filepath.Join(wd, path), path, infos.IsDir())
			if err == filepath.SkipDir {
				walker.SkipDir()
			} else if err != nil {
				return err
			}
		}
	}
	return nil
}

func SFTPGetAuth(ctx context.Context, srcs []string, params SSHParams, auth []ssh.AuthMethod, cb Callback, l *zap.SugaredLogger) error {
	if len(srcs) == 0 {
		return nil
	}
	if len(auth) == 0 {
		return errors.New("no auth method")
	}
	cfg := gssh.Config{
		User: params.LoginName,
		Host: params.Host,
		Port: params.Port,
		Auth: auth,
	}
	hkcb, err := gssh.MakeHostKeyCallback(params.Insecure, l)
	if err != nil {
		return err
	}
	cfg.HostKey = hkcb
	client, err := gssh.SFTP(cfg)
	if err != nil {
		return err
	}

	stopping := make(chan struct{})
	defer close(stopping)
	go func() {
		select {
		case <-ctx.Done():
		case <-stopping:
		}
		_ = client.Close()
	}()

	sendFile := func(base, filename string, st os.FileInfo) error {
		relFilename, err := filepath.Rel(base, filename)
		if err != nil {
			return err
		}
		f, err := client.Open(filename)
		if err != nil {
			return err
		}
		err = cb(false, false, relFilename, st.Mode().Perm(), st.ModTime(), time.Now(), f)
		_ = f.Close()
		return err
	}

	var sendDir func(string, string, os.FileInfo) error
	sendDir = func(base, dirname string, st os.FileInfo) error {
		infos, err := client.ReadDir(dirname)
		if err != nil {
			return err
		}
		relDirname, err := filepath.Rel(base, dirname)
		if err != nil {
			return err
		}
		err = cb(true, false, relDirname, st.Mode().Perm(), st.ModTime(), time.Now(), nil)
		if err != nil {
			return err
		}
		for _, info := range infos {
			if info.IsDir() {
				if err := sendDir(base, filepath.Join(dirname, info.Name()), info); err != nil {
					return err
				}
			} else if info.Mode().IsRegular() {
				filename := filepath.Join(dirname, info.Name())
				if err := sendFile(base, filename, info); err != nil {
					return err
				}
			}
		}
		return cb(true, true, relDirname, st.Mode().Perm(), st.ModTime(), time.Time{}, nil)
	}

	for _, src := range srcs {
		stats, err := client.Stat(src)
		if err != nil {
			return err
		}
		if stats.IsDir() {
			if err := sendDir(filepath.Dir(src), src, stats); err != nil {
				return err
			}
		} else if stats.Mode().IsRegular() {
			if err := sendFile(filepath.Dir(src), src, stats); err != nil {
				return err
			}
		}
	}

	return nil
}

func ScpGetAuth(ctx context.Context, srcs []string, params SSHParams, auth []ssh.AuthMethod, cb Callback, l *zap.SugaredLogger) error {
	if len(srcs) == 0 {
		return nil
	}
	if len(auth) == 0 {
		return errors.New("no auth method")
	}
	cfg := gssh.Config{
		User: params.LoginName,
		Host: params.Host,
		Port: params.Port,
		Auth: auth,
	}
	hkcb, err := gssh.MakeHostKeyCallback(params.Insecure, l)
	if err != nil {
		return err
	}
	cfg.HostKey = hkcb

	for _, source := range srcs {
		err := receive(ctx, cfg, source, cb, l)
		if err != nil {
			return err
		}
	}
	return nil
}

func makeAuthCertificate(privkey, cert *memguard.LockedBuffer) (ssh.AuthMethod, error) {
	c, err := gssh.ParseCertificate(cert.Buffer())
	if err != nil {
		return nil, err
	}
	s, err := ssh.ParsePrivateKey(privkey.Buffer())
	if err != nil {
		return nil, err
	}
	signer, err := ssh.NewCertSigner(c, s)
	if err != nil {
		return nil, err
	}
	return ssh.PublicKeys(signer), nil
}

func ScpGet(ctx context.Context, srcs []string, params SSHParams, privkey, cert *memguard.LockedBuffer, cb Callback, l *zap.SugaredLogger) error {
	a, err := makeAuthCertificate(privkey, cert)
	if err != nil {
		return err
	}
	return ScpGetAuth(ctx, srcs, params, []ssh.AuthMethod{a}, cb, l)
}

func SFTPGet(ctx context.Context, srcs []string, params SSHParams, privkey, cert *memguard.LockedBuffer, cb Callback, l *zap.SugaredLogger) error {
	a, err := makeAuthCertificate(privkey, cert)
	if err != nil {
		return err
	}
	return SFTPGetAuth(ctx, srcs, params, []ssh.AuthMethod{a}, cb, l)
}

type ListCallback func(path, relName string, isDir bool) error

func SFTPList(ctx context.Context, params SSHParams, privkey, cert *memguard.LockedBuffer, l *zap.SugaredLogger, cb ListCallback) error {
	a, err := makeAuthCertificate(privkey, cert)
	if err != nil {
		return err
	}
	return SFTPListAuth(ctx, params, []ssh.AuthMethod{a}, l, cb)
}

func receive(ctx context.Context, cfg gssh.Config, src string, cb Callback, l *zap.SugaredLogger) error {
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
	clt, err := gssh.StartCommand(lctx, cfg, command)
	if err != nil {
		return err
	}
	go func() {
		_, _ = io.Copy(os.Stderr, bufio.NewReader(clt.Stderr))
	}()
	err = receiveOne(clt.Stdin, bufio.NewReader(clt.Stdout), src, "", cb, l)
	if err != nil {
		_ = clt.Stdin.Close()
		cancel()
		_ = clt.Wait()
		return err
	}
	_ = clt.Stdin.Close()
	return clt.Wait()
}

func receiveOne(stdin io.Writer, stdout *bufio.Reader, src, lPath string, cb Callback, l *zap.SugaredLogger) error {
	_ = ack(stdin)
	var mtime time.Time
	var atime time.Time
	var iter int
	for {
		iter++
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
			if iter == 1 {
				return errors.New(string(line))
			}
			return fmt.Errorf("unexpected response: %s", string(line))
		}
		// TODO: check that the downloaded names match the user request
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
		if target == "" || target == "." || target == ".." || strings.Contains(target, "/") {
			return fmt.Errorf("unexpected filename: %s", target)
		}

		now := time.Now()
		if mtime.IsZero() {
			mtime = now
		}
		if atime.IsZero() {
			atime = now
		}

		if msg == 'D' {
			dirPath := filepath.Join(lPath, target)
			l.Debugw("scp received dir", "target", target, "lpath", lPath, "dirpath", dirPath)
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
			mtime = time.Time{}
			atime = time.Time{}
			continue
		}

		// if msg == 'C'
		_ = ack(stdin)
		lr := &io.LimitedReader{R: stdout, N: size}
		filePath := filepath.Join(lPath, target)
		l.Debugw("scp received file", "target", target, "lpath", lPath, "filepath", filePath)
		err = cb(false, false, filePath, os.FileMode(perms), mtime, atime, lr)
		if err != nil {
			return err
		}
		_, _ = io.Copy(ioutil.Discard, lr)
		mtime = time.Time{}
		atime = time.Time{}
		_, _, _ = readResponse(stdout)
		_ = ack(stdin)
	}
}

func ack(w io.Writer) error {
	_, err := fmt.Fprint(w, "\x00")
	return err
}
