// Package ssh is a helper for working with ssh in go.  The client implementation
// is a modified version of `docker/machine/libmachine/ssh/client.go` and only
// uses golang's native ssh client. It has also been improved to resize the tty
// accordingly. The key functions are meant to be used by either client or server
// and will generate/store keys if not found.
package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/pkg/term"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

type Client struct {
	Config
	openSession *ssh.Session
	openClient  *ssh.Client
	sync.Mutex
	stopping chan struct{}
}

type Config struct {
	User          string              // username to connect as, required
	Host          string              // hostname to connect to, required
	ClientVersion string              // ssh client version, "SSH-2.0-Go" by default
	Port          int                 // port to connect to, 22 by default
	Auth          []ssh.AuthMethod    // authentication methods to use
	Timeout       time.Duration       // connect timeout, 30s by default
	DialRetry     int                 // number of dial retries, 0 (no retries) by default
	HostKey       ssh.HostKeyCallback // callback for verifying server keys, ssh.InsecureIgnoreHostKey by default
}

func (cfg Config) Version() string {
	if cfg.ClientVersion != "" {
		return cfg.ClientVersion
	}
	return "SSH-2.0-Go"
}

func (cfg Config) port() int {
	if cfg.Port != 0 {
		return cfg.Port
	}
	return 22
}

func (cfg Config) timeout() time.Duration {
	if cfg.Timeout != 0 {
		return cfg.Timeout
	}
	return 15 * time.Second
}

func (cfg Config) hostKey() ssh.HostKeyCallback {
	if cfg.HostKey != nil {
		return cfg.HostKey
	}
	return ssh.InsecureIgnoreHostKey()
}

func (cfg Config) Addr() string {
	return net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.port()))
}

func (cfg Config) Native() *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            cfg.Auth,
		ClientVersion:   cfg.Version(),
		HostKeyCallback: cfg.hostKey(),
		Timeout:         cfg.timeout(),
	}
}

// NewClient creates a new Client using the golang ssh library.
func NewClient(cfg Config) *Client {
	return &Client{
		Config: cfg,
	}
}

// Start starts the specified command without waiting for it to finish. You
// have to call the Wait function for that.
func (client *Client) Start(ctx context.Context, command string) (io.WriteCloser, io.Reader, io.Reader, error) {
	client.Lock()
	defer client.Unlock()
	if client.openSession != nil {
		return nil, nil, nil, errors.New("client already started")
	}
	stopping := make(chan struct{})
	client.stopping = stopping
	session, conn, err := newSession(client.Config)
	if err != nil {
		return nil, nil, nil, err
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = conn.Close()
		return nil, nil, nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = conn.Close()
		return nil, nil, nil, err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		_ = session.Close()
		_ = conn.Close()
		return nil, nil, nil, err
	}
	err = session.Start(command)
	if err != nil {
		_ = session.Close()
		_ = conn.Close()
		return nil, nil, nil, err
	}

	go func() {
		select {
		case <-ctx.Done():
			_ = session.Close()
		case <-stopping:
		}
	}()

	client.openSession = session
	client.openClient = conn
	return stdin, stdout, stderr, nil
}

// Wait waits for the command started by the Start function to exit. The
// returned error follows the same logic as in the exec.Cmd.Wait function.
func (client *Client) Wait() (err error) {
	client.Lock()
	sess := client.openSession
	if sess != nil {
		client.Unlock()
		err = sess.Wait()
		client.Lock()
		if client.stopping != nil {
			close(client.stopping)
			client.stopping = nil
		}
		if client.openSession != nil {
			_ = client.openSession.Close()
			client.openSession = nil
		}
		if client.openClient != nil {
			_ = client.openClient.Close()
			client.openClient = nil
		}
		client.Unlock()
		return wrapError(err)
	}
	if client.openClient != nil {
		_ = client.openClient.Close()
		client.openClient = nil
	}
	client.Unlock()
	return nil
}

func newSession(config Config) (*ssh.Session, *ssh.Client, error) {
	var conn *ssh.Client
	var err error
	for i := config.DialRetry + 1; i > 0; i-- {
		conn, err = ssh.Dial("tcp", config.Addr(), config.Native())
		if err == nil {
			break
		}
		time.Sleep(3 * time.Second) // backoff?
	}
	if err != nil {
		return nil, nil, err
	}
	session, err := conn.NewSession()
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return session, conn, nil
}

// Output returns the output of the command run on the remote host.
func Output(ctx context.Context, config Config, command string, stdout, stderr io.Writer) error {
	session, conn, err := newSession(config)
	if err != nil {
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
	session, conn, err := newSession(config)
	if err != nil {
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
	session, conn, err := newSession(config)
	if err != nil {
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
