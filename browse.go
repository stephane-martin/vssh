package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/pkg/sftp"
)

type sftpFS struct {
	wd     string
	client *sftp.Client
	out    *logWriter
}

type sftpFile struct {
	remotePath string
	tmpPath    string
	remoteFile *sftp.File
	tmpFile    *os.File
	client     *sftp.Client
	out        *logWriter
}

func (f *sftpFile) Read(p []byte) (n int, err error) {
	if f.tmpFile == nil {
		return 0, fmt.Errorf("is a directory: %s", f.remotePath)
	}
	return f.tmpFile.Read(p)
}

func (f *sftpFile) Close() error {
	var err error
	if f.tmpFile != nil {
		err = f.tmpFile.Close()
	}
	_ = f.remoteFile.Close()
	if f.tmpPath != "" {
		_ = os.Remove(f.tmpPath)
		_ = os.RemoveAll(filepath.Dir(f.tmpPath))
	}
	return err
}

func (f *sftpFile) Seek(offset int64, whence int) (int64, error) {
	if f.tmpFile == nil {
		return 0, fmt.Errorf("is a directory: %s", f.remotePath)
	}
	return f.tmpFile.Seek(offset, whence)
}

func (f *sftpFile) Readdir(count int) ([]os.FileInfo, error) {
	//fmt.Println("readdir", count, f.remotePath)
	return f.client.ReadDir(f.remotePath)
}

func (f *sftpFile) Stat() (os.FileInfo, error) {
	return f.remoteFile.Stat()
}

func (fs *sftpFS) Open(name string) (http.File, error) {
	remotePath := filepath.Join(fs.wd, path.Clean("/"+name))
	remoteFile, err := fs.client.Open(remotePath)
	if err != nil {
		return nil, err
	}
	stats, err := remoteFile.Stat()
	if err != nil {
		return nil, err
	}
	if stats.IsDir() {
		return &sftpFile{
			remotePath: remotePath,
			tmpPath:    "",
			remoteFile: remoteFile,
			tmpFile:    nil,
			client:     fs.client,
			out:        fs.out,
		}, nil
	}
	tmpDir, err := ioutil.TempDir("", "vssh-http-tempfile")
	if err != nil {
		_ = remoteFile.Close()
		return nil, err
	}
	tmpPath := filepath.Join(tmpDir, filepath.Base(remotePath))
	fs.out.Print("Open [blue]%s[-]: download [blue]%s[-] to [blue]%s[-]", name, remotePath, tmpPath)
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		_ = remoteFile.Close()
		_ = os.RemoveAll(tmpDir)
		return nil, err
	}
	clean := func() {
		_ = tmpFile.Close()
		_ = remoteFile.Close()
		_ = os.Remove(tmpPath)
		_ = os.RemoveAll(tmpDir)
	}
	_, err = io.Copy(tmpFile, remoteFile)
	if err != nil {
		clean()
		return nil, err
	}
	_, err = tmpFile.Seek(0, 0)
	if err != nil {
		clean()
		return nil, err
	}
	err = os.Chtimes(tmpPath, time.Now(), stats.ModTime())
	if err != nil {
		clean()
		return nil, err
	}
	return &sftpFile{
		remotePath: remotePath,
		remoteFile: remoteFile,
		tmpPath:    tmpPath,
		tmpFile:    tmpFile,
		client:     fs.client,
		out:        fs.out,
	}, nil
}

type logWriter struct {
	out io.Writer
	mu  sync.Mutex
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	n, err := w.out.Write(p)
	w.mu.Unlock()
	return n, err
}

func (w *logWriter) Print(s string, args ...interface{}) (int, error) {
	if s == "" {
		return 0, nil
	}
	w.mu.Lock()
	n, err := fmt.Fprintf(w.out, s+"\n", args...)
	w.mu.Unlock()
	return n, err
}

func browseDir(ctx context.Context, client *sftp.Client, addr string, wd string, out io.Writer) error {
	logOut := &logWriter{out: out}
	fs := &sftpFS{wd: wd, client: client, out: logOut}
	h := http.FileServer(fs)
	h = stripModifiedHeader(h)
	h = LoggingHandler(logOut, h)
	server := &http.Server{
		Addr:     addr,
		Handler:  h,
		ErrorLog: log.New(logOut, "", 0),
	}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	err := server.ListenAndServe()
	if err == context.Canceled {
		return nil
	}
	return err
}

func stripModifiedHeader(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Del("If-Modified-Since")
		r.Header.Del("If-Unmodified-Since")
		h.ServeHTTP(w, r)
	})
}
