// Package ssh is a helper for working with ssh in go.
package ssh

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/moby/moby/pkg/term"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

type Client struct {
	Cfg     Config
	Session *ssh.Session
	Conn    *ssh.Client
	Stdin   io.WriteCloser
	Stdout  io.Reader
	Stderr  io.Reader
	sync.Mutex
	stopping chan struct{}
}

type Config struct {
	User          string              // username to connect as, required
	Host          string              // hostname to connect to, required
	ClientVersion string              // ssh client version, "SSH-2.0-Go" by default
	Port          int                 // port to connect to, 22 by default
	Auth          []ssh.AuthMethod    // authentication methods to use
	HostKey       ssh.HostKeyCallback // callback for verifying server keys, ssh.InsecureIgnoreHostKey by default
	HTTPProxy     *url.URL
}

func (cfg Config) Version() string {
	if cfg.ClientVersion != "" {
		return cfg.ClientVersion
	}
	return "SSH-2.0-Go"
}

func (cfg Config) GetPort() int {
	if cfg.Port != 0 {
		return cfg.Port
	}
	return 22
}

func (cfg Config) GetHostKeyCallback() ssh.HostKeyCallback {
	if cfg.HostKey != nil {
		return cfg.HostKey
	}
	return ssh.InsecureIgnoreHostKey()
}

func (cfg Config) GetAddr() string {
	return net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.GetPort()))
}

func (cfg Config) ToNatives() []*ssh.ClientConfig {
	natives := make([]*ssh.ClientConfig, 0, len(cfg.Auth))
	for _, auth := range cfg.Auth {
		natives = append(natives, &ssh.ClientConfig{
			User:            cfg.User,
			Auth:            []ssh.AuthMethod{auth},
			ClientVersion:   cfg.Version(),
			HostKeyCallback: cfg.GetHostKeyCallback(),
		})
	}
	return natives
}

func SFTP(ctx context.Context, cfg Config) (*sftp.Client, error) {
	conn, err := Dial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return sftp.NewClient(conn)
}

// StartCommand starts the specified command without waiting for it to finish. You
// have to call the Wait function for that.
func StartCommand(ctx context.Context, cfg Config, command string) (*Client, error) {
	client := &Client{Cfg: cfg}
	conn, err := Dial(ctx, client.Cfg)
	if err != nil {
		return nil, err
	}
	session, err := conn.NewSession()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = conn.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = conn.Close()
		return nil, err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		_ = session.Close()
		_ = conn.Close()
		return nil, err
	}
	err = session.Start(command)
	if err != nil {
		_ = session.Close()
		_ = conn.Close()
		return nil, err
	}

	stopping := make(chan struct{})
	client.stopping = stopping

	go func() {
		select {
		case <-ctx.Done():
			_ = session.Close()
		case <-stopping:
		}
	}()

	client.Session = session
	client.Conn = conn
	client.Stdout = stdout
	client.Stderr = stderr
	client.Stdin = stdin
	return client, nil
}

// Wait waits for the command started by the Start function to exit. The
// returned error follows the same logic as in the exec.Cmd.Wait function.
func (client *Client) Wait() (err error) {
	client.Lock()
	sess := client.Session
	if sess != nil {
		client.Unlock()
		err = sess.Wait()
		client.Lock()
		if client.stopping != nil {
			close(client.stopping)
			client.stopping = nil
		}
		if client.Session != nil {
			_ = client.Session.Close()
			client.Session = nil
		}
		if client.Conn != nil {
			_ = client.Conn.Close()
			client.Conn = nil
		}
		client.Unlock()
		return wrapError(err)
	}
	if client.Conn != nil {
		_ = client.Conn.Close()
		client.Conn = nil
	}
	client.Unlock()
	return nil
}

func Dial(ctx context.Context, config Config) (*ssh.Client, error) {
	var err error
	var conn *ssh.Client
	var proxy *HTTPConnectProxy
	if config.HTTPProxy != nil {
		proxy = NewHTTPConnectProxy(config.HTTPProxy)
	}
	for _, native := range config.ToNatives() {
		conn, err = dial(ctx, config.GetAddr(), native, proxy)
		if err == nil {
			return conn, nil
		}
	}
	return nil, err
}

func dial(ctx context.Context, addr string, config *ssh.ClientConfig, httpProxy *HTTPConnectProxy) (*ssh.Client, error) {
	var dialC func(ctx context.Context, network, address string) (net.Conn, error)
	if httpProxy == nil {
		var d net.Dialer
		dialC = d.DialContext
	} else {
		dialC = httpProxy.DialContext
	}
	conn, err := dialC(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		return nil, err
	}
	client := ssh.NewClient(c, chans, reqs)
	if ctx != nil {
		select {
		case <-ctx.Done():
			_ = client.Close()
			return nil, ctx.Err()
		default:
		}
	}
	return client, nil
}

// Output returns the output of the command run on the remote host.
func Output(ctx context.Context, config Config, command string, stdout, stderr io.Writer) error {
	conn, err := Dial(ctx, config)
	if err != nil {
		return err
	}
	session, err := conn.NewSession()
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer func() {
		_ = session.Close()
		_ = conn.Close()
	}()
	session.Stdout = stdout
	session.Stderr = stderr
	lctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-lctx.Done():
		case <-ctx.Done():
			_ = session.Close()
		}
	}()
	err = wrapError(session.Run(command))
	cancel()
	return err
}

// Output returns the output of the command run on the remote host as well as a pty.
func OutputWithPty(ctx context.Context, config Config, command string, stdout, stderr io.Writer) error {
	conn, err := Dial(ctx, config)
	if err != nil {
		return err
	}
	session, err := conn.NewSession()
	if err != nil {
		_ = conn.Close()
		return err
	}

	defer func() {
		_ = session.Close()
		_ = conn.Close()
	}()

	termWidth, termHeight, err := terminal.GetSize(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	// request tty -- fixes error with hosts that use
	// "Defaults requiretty" in /etc/sudoers - I'm looking at you RedHat
	if err := session.RequestPty("xterm", termHeight, termWidth, modes); err != nil {
		return err
	}

	session.Stdout = stdout
	session.Stderr = stderr

	lctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-lctx.Done():
		case <-ctx.Done():
			_ = session.Close()
		}
	}()

	err = wrapError(session.Run(command))
	cancel()
	return err
}

// Shell requests a shell from the remote. If an arg is passed, it tries to
// exec them on the server.
func Shell(ctx context.Context, config Config, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	var (
		termWidth, termHeight = 80, 24
	)
	conn, err := Dial(ctx, config)
	if err != nil {
		return err
	}
	session, err := conn.NewSession()
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer func() {
		_ = session.Close()
		_ = conn.Close()
	}()

	session.Stdout = stdout
	session.Stderr = stderr
	session.Stdin = stdin

	modes := ssh.TerminalModes{
		ssh.ECHO: 1,
	}

	fd := os.Stdin.Fd()

	if term.IsTerminal(fd) {
		oldState, err := term.MakeRaw(fd)
		if err != nil {
			return err
		}

		defer func() { _ = term.RestoreTerminal(fd, oldState) }()

		winsize, err := term.GetWinsize(fd)
		if err == nil {
			termWidth = int(winsize.Width)
			termHeight = int(winsize.Height)
		}
	}

	if err := session.RequestPty("xterm", termHeight, termWidth, modes); err != nil {
		return err
	}

	lctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-lctx.Done():
		case <-ctx.Done():
			_ = session.Close()
		}
	}()

	if len(args) != 0 {
		err := wrapError(session.Run(strings.Join(args, " ")))
		cancel()
		return err
	}

	err = wrapError(session.Shell())
	if err != nil {
		cancel()
		return err
	}
	// monitor for sigwinch
	go monWinCh(session, os.Stdout.Fd())
	err = wrapError(session.Wait())
	cancel()
	return err
}
