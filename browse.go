package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
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
	remoteFile *sftp.File
	client     *sftp.Client
	out        *logWriter
}

func (f *sftpFile) Read(p []byte) (n int, err error) {
	return f.remoteFile.Read(p)
}

func (f *sftpFile) Close() error {
	return f.remoteFile.Close()
}

func (f *sftpFile) Readdir() ([]os.FileInfo, error) {
	return f.client.ReadDir(f.remotePath)
}

func (f *sftpFile) Stat() (os.FileInfo, error) {
	return f.remoteFile.Stat()
}

func (fs *sftpFS) Open(name string) (*sftpFile, error) {
	remotePath := filepath.Join(fs.wd, path.Clean("/"+name))
	remoteFile, err := fs.client.Open(remotePath)
	if err != nil {
		return nil, err
	}
	return &sftpFile{
		remotePath: remotePath,
		remoteFile: remoteFile,
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
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := r.URL.Path
		if !strings.HasPrefix(upath, "/") {
			upath = "/" + upath
			r.URL.Path = upath
		}
		serveFile(w, r, fs, path.Clean(upath), true)
	})
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

func dirList(w http.ResponseWriter, r *http.Request, f *sftpFile) {
	dirs, err := f.Readdir()
	if err != nil {
		//logf(r, "http: error reading directory: %v", err)
		//Error(w, "Error reading directory", StatusInternalServerError)
		return
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name() < dirs[j].Name() })

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<pre>\n")
	for _, d := range dirs {
		name := d.Name()
		if d.IsDir() {
			name += "/"
		}
		// name may contain '?' or '#', which must be escaped to remain
		// part of the URL path, and not indicate the start of a query
		// string or fragment.
		url := url.URL{Path: name}
		fmt.Fprintf(w, "<a href=\"%s\">%s</a>\n", url.String(), htmlReplacer.Replace(name))
	}
	fmt.Fprintf(w, "</pre>\n")
}

var htmlReplacer = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	// "&#34;" is shorter than "&quot;".
	`"`, "&#34;",
	// "&#39;" is shorter than "&apos;" and apos was not in HTML until HTML5.
	"'", "&#39;",
)

func serveFile(w http.ResponseWriter, r *http.Request, fs *sftpFS, name string, redirect bool) {
	const indexPage = "/index.html"

	// redirect .../index.html to .../
	// can't use Redirect() because that would make the path absolute,
	// which would be a problem running under StripPrefix
	if strings.HasSuffix(r.URL.Path, indexPage) {
		localRedirect(w, r, "./")
		return
	}

	f, err := fs.Open(name)
	if err != nil {
		//msg, code := toHTTPError(err)
		//Error(w, msg, code)
		return
	}
	defer f.Close()

	d, err := f.Stat()
	if err != nil {
		//msg, code := toHTTPError(err)
		//Error(w, msg, code)
		return
	}

	if redirect {
		// redirect to canonical path: / at end of directory url
		// r.URL.Path always begins with /
		url := r.URL.Path
		if d.IsDir() {
			if url[len(url)-1] != '/' {
				localRedirect(w, r, path.Base(url)+"/")
				return
			}
		} else {
			if url[len(url)-1] == '/' {
				localRedirect(w, r, "../"+path.Base(url))
				return
			}
		}
	}

	// redirect if the directory name doesn't end in a slash
	if d.IsDir() {
		url := r.URL.Path
		if url[len(url)-1] != '/' {
			localRedirect(w, r, path.Base(url)+"/")
			return
		}
	}

	// use contents of index.html for directory, if present
	if d.IsDir() {
		index := strings.TrimSuffix(name, "/") + indexPage
		ff, err := fs.Open(index)
		if err == nil {
			defer ff.Close()
			dd, err := ff.Stat()
			if err == nil {
				d = dd
				f = ff
			}
		}
	}

	// Still a directory? (we didn't find an index.html file)
	if d.IsDir() {
		dirList(w, r, f)
		return
	}

	// serveContent will check modification time
	sizeFunc := func() (int64, error) { return d.Size(), nil }
	serveContent(w, r, d.Name(), d.ModTime(), sizeFunc, f)
}

func localRedirect(w http.ResponseWriter, r *http.Request, newPath string) {
	if q := r.URL.RawQuery; q != "" {
		newPath += "?" + q
	}
	w.Header().Set("Location", newPath)
	w.WriteHeader(http.StatusMovedPermanently)
}

func serveContent(w http.ResponseWriter, r *http.Request, name string, modtime time.Time, sizeFunc func() (int64, error), content io.Reader) {
	code := http.StatusOK

	ctype := mime.TypeByExtension(filepath.Ext(name))
	if ctype == "" {
		// read a chunk to decide between utf-8 text and binary
		var buf [512]byte
		n, _ := io.ReadFull(content, buf[:])
		ctype = http.DetectContentType(buf[:n])
		content = io.MultiReader(bytes.NewReader(buf[:n]), content)
	}
	w.Header().Set("Content-Type", ctype)

	size, err := sizeFunc()
	if err != nil {
		//Error(w, err.Error(), StatusInternalServerError)
		return
	}

	w.WriteHeader(code)

	if r.Method != "HEAD" {
		_, _ = io.CopyN(w, content, size)
	}
}
