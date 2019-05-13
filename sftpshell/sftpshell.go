package sftpshell

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/stephane-martin/vssh/remoteops"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cheggaaa/pb"
	"github.com/mattn/go-shellwords"
	"github.com/pkg/sftp"
	"github.com/scylladb/go-set/strset"
	"golang.org/x/crypto/ssh/terminal"
)

// TODO: deal with symlinks correctly

type only int

const (
	filesAndDirs only = iota
	onlyFiles
	onlyDirs
)

type command func([]string, *strset.Set) error

type cmpl func([]string, bool) []string

type ShellState struct {
	LocalWD       string
	RemoteWD      string
	initRemoteWD  string
	client        *sftp.Client
	methods       map[string]command
	completes     map[string]cmpl
	externalPager bool
	tempfiles     *strset.Set
	info          func(string, ...interface{})
	err           func(string, ...interface{})
	out           io.Writer
	environ       map[string]string
	report        bool
}

func NewShellState(client *sftp.Client, externalPager bool, out io.Writer, infoFunc func(string, ...interface{}), errFunc func(string, ...interface{})) (*ShellState, error) {
	localwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	remotewd, err := client.Getwd()
	if err != nil {
		return nil, err
	}
	s := &ShellState{
		LocalWD:       localwd,
		RemoteWD:      remotewd,
		initRemoteWD:  remotewd,
		client:        client,
		externalPager: externalPager,
		tempfiles:     strset.New(),
		out:           out,
		environ:       make(map[string]string),
		report:        true,
	}

	s.info = func(f string, args ...interface{}) {
		if s.report {
			infoFunc(f, args...)
		}
	}

	s.err = func(f string, args ...interface{}) {
		if s.report {
			errFunc(f, args...)
		}
	}

	s.methods = map[string]command{
		"less":      s.less,
		"lless":     s.lless,
		"ls":        s.ls,
		"lls":       s.lls,
		"ll":        s.ll,
		"lll":       s.lll,
		"llist":     s.llist,
		"list":      s.list,
		"cd":        s.cd,
		"lcd":       s.lcd,
		"edit":      s.edit,
		"ledit":     s.ledit,
		"open":      s.open,
		"lopen":     s.lopen,
		"exit":      s.exit,
		"logout":    s.exit,
		"q":         s.exit,
		":q":        s.exit,
		"pwd":       s.pwd,
		"lpwd":      s.lpwd,
		"get":       s.get,
		"put":       s.put,
		"mkdir":     s.mkdir,
		"mkdirall":  s.mkdirall,
		"lmkdir":    s.lmkdir,
		"lmkdirall": s.lmkdirall,
		"rm":        s.rm,
		"lrm":       s.lrm,
		"rmdir":     s.rmdir,
		"lrmdir":    s.lrmdir,
		"lmv":       s.lmv,
		"mv":        s.mv,
		"lcp":       s.lcp,
		"cp":        s.cp,
		"browse":    s.browse,
		"lbrowse":   s.lbrowse,
		"env":       s.env,
		"set":       s.set,
		"unset":     s.unset,
		"cowsay":    s.cowsay,
	}
	s.completes = map[string]cmpl{
		"cd":     s.completeCd,
		"lcd":    s.completeLcd,
		"less":   s.completeLess,
		"lless":  s.completeLless,
		"open":   s.completeOpen,
		"lopen":  s.completeLopen,
		"rmdir":  s.completeRmdir,
		"lrmdir": s.completeLrmdir,
		"lrm":    s.completeLrm,
		"rm":     s.completeRm,
		"ledit":  s.completeLedit,
		"edit":   s.completeEdit,
		"put":    s.completePut,
		"get":    s.completeGet,
	}
	for _, e := range os.Environ() {
		spl := strings.SplitN(e, "=", 2)
		if spl[0] == "" {
			continue
		}
		if len(spl) == 1 {
			s.environ[spl[0]] = ""
		}
		if len(spl) == 2 {
			s.environ[spl[0]] = spl[1]
		}
	}
	return s, nil
}

func (s *ShellState) Getenv(k string) string {
	return s.environ[k]
}

func (s *ShellState) Close() error {
	var err error
	s.tempfiles.Each(func(fname string) bool {
		e := os.Remove(fname)
		if e != nil && err == nil {
			err = e
		}
		return true
	})
	return err
}

func (s *ShellState) Width() int {
	width, _, err := terminal.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 80
	}
	return width
}

func (s *ShellState) exit(_ []string, flags *strset.Set) error {
	return io.EOF
}

func (s *ShellState) Complete(cmd string, args []string, lastSpace bool) []string {
	fun := s.completes[cmd]
	if fun == nil {
		return nil
	}
	return fun(args, lastSpace)
}

func (s *ShellState) Dispatch(line string) error {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	p := shellwords.NewParser()
	p.ParseEnv = true
	p.Getenv = s.Getenv
	args, err := p.Parse(line)
	if err != nil {
		return err
	}
	if p.Position != -1 {
		return errors.New("incomplete parsing error")
	}
	if len(args) == 0 {
		return nil
	}
	cmd := args[0]

	if strings.HasPrefix(cmd, "!") {
		cmd = strings.Trim(cmd, "!")
		if cmd == "" {
			return nil
		}
		return s.external(cmd, args[1:])
	}

	cmd = strings.ToLower(cmd)
	var posargs []string
	sflags := strset.New()
	for _, s := range args[1:] {
		if strings.HasPrefix(s, "-") {
			sflags.Add(strings.TrimLeft(s, "-"))
		} else {
			posargs = append(posargs, s)
		}
	}
	fun := s.methods[cmd]
	if fun == nil {
		return fmt.Errorf("unknown command: %s", cmd)
	}
	return fun(posargs, sflags)
}

func join(dname, fname string) string {
	if strings.HasPrefix(fname, "/") {
		return fname
	}
	if strings.HasSuffix(fname, "/") {
		return filepath.Join(dname, fname) + "/"
	}
	return filepath.Join(dname, fname)
}



var spinner = []rune("◐◓◑◒")

func (s *ShellState) external(cmd string, args []string) error {
	ex, err := exec.LookPath(cmd)
	if err != nil {
		return err
	}
	e := make([]string, 0, len(s.environ))
	for k, v := range s.environ {
		e = append(e, fmt.Sprintf("%s=%s", k, v))
	}
	c := exec.Command(ex, args...)
	c.Env = e
	c.Dir = s.LocalWD
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}


func newBar(size int64) *pb.ProgressBar {
	bar := pb.New(int(size)).SetUnits(pb.U_BYTES).SetRefreshRate(time.Second).SetMaxWidth(72)
	bar.ShowElapsedTime = false
	bar.ShowFinalTime = false
	bar.ShowTimeLeft = false
	bar.Start()
	return bar
}

func (s *ShellState) pwd(args []string, flags *strset.Set) error {
	if len(args) != 0 {
		return errors.New("pwd takes no argument")
	}
	fmt.Fprintln(s.out, s.RemoteWD)
	return nil
}

func (s *ShellState) lpwd(args []string, flags *strset.Set) error {
	if len(args) != 0 {
		return errors.New("lpwd takes no argument")
	}
	fmt.Fprintln(s.out, s.LocalWD)
	return nil
}



func nonExistingFiles(args []string, wd string, client *sftp.Client) (*strset.Set, error) {
	var stat func(string) (os.FileInfo, error)
	if client == nil {
		stat = os.Stat
	} else {
		stat = client.Stat
	}
	s := strset.New()
	for _, arg := range args {
		if !remoteops.HasMeta(arg) {
			arg = join(wd, arg)
			_, err := stat(arg)
			if err != nil {
				if !os.IsNotExist(err) {
					return nil, err
				}
				s.Add(arg)
			}
		}
	}
	return s, nil
}

func findMatches(args []string, wd string, client *sftp.Client, o only) (*strset.Set, error) {
	allmatches := strset.New()
	if len(args) == 0 {
		return allmatches, nil
	}

	var glob func(string, string) ([]string, error)
	var stat func(string) (os.FileInfo, error)
	if client == nil {
		glob = remoteops.LocalGlob
		stat = os.Stat
	} else {
		glob = func(wd string, pattern string) ([]string, error) {
			return remoteops.SFTPGlob(wd, client, pattern)
		}
		stat = client.Stat
	}

	for _, pattern := range args {
		// list matching files
		matches, err := glob(wd, pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern %s: %s", pattern, err)
		}
		for _, match := range matches {
			match = join(wd, match)
			if o == filesAndDirs {
				allmatches.Add(match)
			} else {
				stats, err := stat(match)
				if err != nil {
					return nil, err
				}
				if o == onlyDirs && stats.IsDir() {
					allmatches.Add(match)
				} else if o == onlyFiles && stats.Mode().IsRegular() {
					allmatches.Add(match)
				}
			}
		}
	}
	return allmatches, nil
}


func hashLocalFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	_, err = io.Copy(h, f)
	if err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}





func base(s string) string {
	s = filepath.Base(s)
	if s == "/" {
		return ""
	}
	return s
}

func rel(base, fname string) string {
	relName, err := filepath.Rel(base, fname)
	if err != nil {
		return fname
	}
	if strings.HasPrefix(relName, "..") {
		return fname
	}
	return relName
}



func isRegularOrLink(info os.FileInfo) bool {
	return info.Mode().IsRegular() || isLink(info)
}

func isLink(info os.FileInfo) bool {
	return (info.Mode() & os.ModeSymlink) != 0
}



func Shorten(path string) string {
	d, f := filepath.Split(path)
	var buf strings.Builder
	var slash bool
	for _, c := range d {
		switch c {
		case '/':
			buf.WriteRune('/')
			slash = true
		default:
			if slash {
				buf.WriteRune(c)
			}
			slash = false
		}
	}
	buf.WriteString(f)
	return buf.String()
}
