package lib

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/awnumar/memguard"
	"github.com/stephane-martin/go-vis"
	gssh "github.com/stephane-martin/golang-ssh"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
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
	Path string
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

// TODO: support globs
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
		return &UploadDirSource{Path: filename}, nil
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

func SFTPPutAuth(ctx context.Context, sources []Source, remotePath string, params SSHParams, auth []ssh.AuthMethod, l *zap.SugaredLogger) error {
	if len(sources) == 0 {
		return nil
	}
	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		remotePath = "."
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

	var destExists, destIsDir bool

	if len(sources) > 1 {
		stats, err := client.Stat(remotePath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("no such file or directory: %s", remotePath)
			}
			return err
		}
		if !stats.IsDir() {
			return fmt.Errorf("not a directory: %s", remotePath)
		}
		destExists = true
		destIsDir = true
	} else {
		_, sourceIsDir := sources[0].(*UploadDirSource)
		stats, err := client.Stat(remotePath)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil {
			destExists = true
			destIsDir = stats.IsDir()
		}
		if sourceIsDir && destExists && !destIsDir {
			return fmt.Errorf("not a directory: %s", remotePath)
		}
	}

	for _, source := range sources {
		var rpath string
		if ds, ok := source.(*UploadDirSource); ok {
			// we upload a directory
			if destExists {
				// destination exists, and is a directory
				rpath = filepath.Join(remotePath, filepath.Base(ds.Path))
			} else {
				// destination does not exist
				// ==> len(sources) is 1
				rpath = remotePath
			}
		}
		if fs, ok := source.(*UploadFileSource); ok {
			if destIsDir {
				// we upload a file, destination exists and is a directory
				rpath = filepath.Join(remotePath, fs.Name)
			} else if destExists {
				// we upload a file, destination exists but is not a directory
				rpath = remotePath
			} else {
				// we upload a file, destination does not exist
				// ==> len(sources) is 1
				rpath = remotePath
			}
		}

		// upload a simple file
		if fs, ok := source.(*UploadFileSource); ok {
			f, err := client.Create(rpath)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, fs.Reader)
			_ = f.Close()
			if err != nil {
				return err
			}
		}

		// upload directory
		if ds, ok := source.(*UploadDirSource); ok {
			if !destExists {
				if err := client.Mkdir(rpath); err != nil {
					return err
				}
			}
			// walk the source directory
			if err := filepath.Walk(ds.Path, func(path string, info os.FileInfo, e error) error {
				if e != nil {
					l.Infow("error walking directory", "path", path, "error", e)
					return nil
				}
				relPath, e := filepath.Rel(ds.Path, path)
				if e != nil {
					return e
				}
				p := filepath.Join(rpath, relPath)
				if info.IsDir() {
					// make the remote directory
					return client.MkdirAll(p)
				} else if info.Mode().IsRegular() {
					fs, e := os.Open(path)
					if e != nil {
						return e
					}
					fd, e := client.Create(p)
					if e != nil {
						_ = fs.Close()
						return e
					}
					_, e = io.Copy(fd, fs)
					_ = fd.Close()
					_ = fs.Close()
					return e
				} else {
					l.Debugw("not uploading irregular file", "filename", path)
				}
				return nil
			}); err != nil {
				return err
			}

		}
	}

	return nil

}

func ScpPutAuth(ctx context.Context, sources []Source, remotePath string, params SSHParams, auth []ssh.AuthMethod, l *zap.SugaredLogger) error {
	if len(sources) == 0 {
		return nil
	}
	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		remotePath = "."
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
	client, err := gssh.StartCommand(ctx, cfg, command)
	if err != nil {
		return err
	}
	go func() {
		_, _ = io.Copy(os.Stderr, bufio.NewReader(client.Stderr))
	}()
	bufStdout := bufio.NewReader(client.Stdout)

	for _, source := range sources {
		err := sendOne(source, client.Stdin, bufStdout, l)
		if err != nil {
			_ = client.Stdin.Close()
			return err
		}
	}
	_ = client.Stdin.Close()
	l.Debugw("waiting for remote process")
	return client.Wait()
}

func ScpPut(ctx context.Context, sources []Source, remotePath string, params SSHParams, privkey, cert *memguard.LockedBuffer, l *zap.SugaredLogger) error {
	lctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel() // close the SSH session
		for _, s := range sources {
			_ = s.Close()
		}
	}()
	a, err := makeAuthCertificate(privkey, cert)
	if err != nil {
		return err
	}
	return ScpPutAuth(lctx, sources, remotePath, params, []ssh.AuthMethod{a}, l)
}

func SFTPPut(ctx context.Context, sources []Source, remotePath string, params SSHParams, privkey, cert *memguard.LockedBuffer, l *zap.SugaredLogger) error {
	lctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel() // close the SSH session
		for _, s := range sources {
			_ = s.Close()
		}
	}()
	a, err := makeAuthCertificate(privkey, cert)
	if err != nil {
		return err
	}
	return SFTPPutAuth(lctx, sources, remotePath, params, []ssh.AuthMethod{a}, l)
}

func sendDir(dirname string, stdin io.WriteCloser, stdout *bufio.Reader, l *zap.SugaredLogger) error {
	stats, err := os.Stat(dirname)
	if err != nil {
		return err
	}
	files, err := ioutil.ReadDir(dirname)
	if err != nil {
		return err
	}

	l.Debugw("uploading directory", "name", dirname)
	sName := filepath.Base(dirname)
	if strings.Contains(sName, "\n") {
		sName = vis.StrVis(sName, vis.VIS_NL)
	}
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

	// TODO: filter out irregular files
	for _, file := range files {
		fname := filepath.Join(dirname, file.Name())
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
		return sendDir(source.Path, stdin, stdout, l)
	}
	source := src.(*UploadFileSource)
	l.Debugw("uploading", "filename", source.Name, "size", source.Size)
	sName := source.Name
	if strings.Contains(source.Name, "\n") {
		sName = vis.StrVis(sName, vis.VIS_NL)
	}

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
