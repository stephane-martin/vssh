package sftpshell

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
	"github.com/scylladb/go-set/strset"
	"github.com/stephane-martin/vssh/params"
	"golang.org/x/sync/errgroup"
	"io"
	"log"
	"mime"
	"net"
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

type bFS struct {
	wd     string
	client *sftp.Client
	out    *logWriter
}

type bFile interface {
	io.Reader
	io.Closer
	Stat() (os.FileInfo, error)
	Readdir(int) ([]os.FileInfo, error)
}

type sftpFile struct {
	remotePath string
	remoteFile *sftp.File
	client     *sftp.Client
}

func (f *sftpFile) Read(p []byte) (n int, err error) {
	return f.remoteFile.Read(p)
}

func (f *sftpFile) Close() error {
	return f.remoteFile.Close()
}

func (f *sftpFile) Readdir(_ int) ([]os.FileInfo, error) {
	return f.client.ReadDir(f.remotePath)
}

func (f *sftpFile) Stat() (os.FileInfo, error) {
	return f.remoteFile.Stat()
}

func (fs *bFS) Open(name string) (bFile, error) {
	p := filepath.Join(fs.wd, path.Clean("/"+name))
	if fs.client == nil {
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		fs.out.Print("Open local file [blue]%s[-]", p)
		return f, nil
	}
	f, err := fs.client.Open(p)
	if err != nil {
		return nil, err
	}
	fs.out.Print("Open SFTP file [blue]%s[-]", p)
	return &sftpFile{
		remotePath: p,
		remoteFile: f,
		client:     fs.client,
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

func BrowseDir(ctx context.Context, client *sftp.Client, addr string, wd string, out io.Writer) error {
	logOut := &logWriter{out: out}
	fs := &bFS{wd: wd, client: client, out: logOut}
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := r.URL.Path
		if !strings.HasPrefix(upath, "/") {
			upath = "/" + upath
			r.URL.Path = upath
		}
		serveFile(w, r, fs, path.Clean(upath), true)
	})
	h = params.LoggingHandler(logOut, h)
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

func dirList(w http.ResponseWriter, r *http.Request, fs *bFS, f bFile) {
	dirs, err := f.Readdir(-1)
	if err != nil {
		fs.out.Print("[red]http: error reading directory: %v[-]", err)
		http.Error(w, "Error reading directory", http.StatusInternalServerError)
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

func serveFile(w http.ResponseWriter, r *http.Request, fs *bFS, name string, redirect bool) {
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
		msg, code := toHTTPError(err)
		http.Error(w, msg, code)
		return
	}
	defer f.Close()

	d, err := f.Stat()
	if err != nil {
		msg, code := toHTTPError(err)
		http.Error(w, msg, code)
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
		dirList(w, r, fs, f)
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

func toHTTPError(err error) (msg string, httpStatus int) {
	if os.IsNotExist(err) {
		return "404 page not found", http.StatusNotFound
	}
	if os.IsPermission(err) {
		return "403 Forbidden", http.StatusForbidden
	}
	return "500 Internal Server Error", http.StatusInternalServerError
}


func _browse(args []string, wd string, client *sftp.Client) error {
	addr := "127.0.0.1:8080"
	if len(args) > 0 {
		_, _, err := net.SplitHostPort(args[0])
		if err != nil {
			return fmt.Errorf("failed to parse HTTP listen address: %s", err)
		}
		addr = args[0]
	}
	app := tview.NewApplication()
	tv := tview.NewTextView()
	tv.SetBorder(true)
	tv.SetDynamicColors(true)
	title := fmt.Sprintf(" browsing directory: %s %%s ", wd)
	tv.SetTitle(fmt.Sprintf(title, string(spinner[0])))
	tv.SetTitleColor(tcell.ColorBlue)
	tv.SetBorderPadding(1, 1, 1, 1)

	tv.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Rune() == 'q' {
			app.Stop()
			return nil
		}
		return event
	})

	ctx := context.Background()
	g, lctx := errgroup.WithContext(ctx)

	pr, pw := io.Pipe()
	r := bufio.NewReader(pr)

	g.Go(func() error {
		_, _ = io.WriteString(tv, fmt.Sprintf("Serve files from [blue]%s[-] on [blue]%s[-]", wd, addr))
		for {
			line, err := r.ReadBytes('\n')
			if len(line) > 0 {
				_, _ = tv.Write(line)
				app.Draw()
			}
			if err != nil {
				return err
			}
		}
	})

	g.Go(func() error {
		err := BrowseDir(lctx, client, addr, wd, pw)
		_ = pw.Close()
		return err
	})

	g.Go(func() error {
		err := app.SetRoot(tv, true).Run()
		if err == nil {
			return context.Canceled
		}
		return err
	})

	g.Go(func() error {
		<-lctx.Done()
		app.Stop()
		return context.Canceled
	})

	g.Go(func() error {
		var i int
		l := len(spinner)
		for {
			select {
			case <-time.After(time.Second):
				i++
				app.QueueUpdateDraw(func() {
					tv.SetTitle(fmt.Sprintf(title, string(spinner[i%l])))
				})
			case <-lctx.Done():
				return context.Canceled
			}
		}
	})

	err := g.Wait()
	if err == context.Canceled {
		return nil
	}
	return err
}

func (s *ShellState) browse(args []string, flags *strset.Set) error {
	return _browse(args, s.RemoteWD, s.client)
}

func (s *ShellState) lbrowse(args []string, flags *strset.Set) error {
	return _browse(args, s.LocalWD, nil)
}