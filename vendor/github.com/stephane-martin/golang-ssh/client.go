// Package ssh is a helper for working with ssh in go.  The client implementation
// is a modified version of `docker/machine/libmachine/ssh/client.go` and only
// uses golang's native ssh client. It has also been improved to resize the tty
// accordingly. The key functions are meant to be used by either client or server
// and will generate/store keys if not found.
package ssh

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"time"

	"github.com/moby/moby/pkg/term"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

type Client struct {
	Config
	openSession *ssh.Session
	openClient  *ssh.Client
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

func (cfg Config) version() string {
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

func (cfg Config) addr() string {
	return net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
}

func (cfg Config) native() *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            cfg.Auth,
		ClientVersion:   cfg.version(),
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

func (client *Client) newSession() (*ssh.Session, *ssh.Client, error) {
	var conn *ssh.Client
	var err error
	addr := client.Config.addr()
	nconfig := client.Config.native()
	for i := client.Config.DialRetry + 1; i > 0; i-- {
		conn, err = ssh.Dial("tcp", addr, nconfig)
		if err == nil {
			break
		}
		time.Sleep(3 * time.Second) // backoff?
	}
	if err != nil {
		return nil, nil, fmt.Errorf("Error attempting SSH client dial: %s", err)
	}
	session, err := conn.NewSession()
	if err != nil {
		return nil, nil, nonil(err, conn.Close())
	}
	return session, conn, nil
}

// Output returns the output of the command run on the remote host.
func (client *Client) Output(command string) (string, error) {
	session, conn, err := client.newSession()
	if err != nil {
		return "", err
	}

	output, err := session.CombinedOutput(command)
	_, _ = session.Close(), conn.Close()
	return string(bytes.TrimSpace(output)), wrapError(err)
}

// Output returns the output of the command run on the remote host as well as a pty.
func (client *Client) OutputWithPty(command string) (string, error) {
	session, conn, err := client.newSession()
	if err != nil {
		return "", nil
	}
	defer session.Close()
	defer conn.Close()

	fd := int(os.Stdin.Fd())

	termWidth, termHeight, err := terminal.GetSize(fd)
	if err != nil {
		return "", err
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	// request tty -- fixes error with hosts that use
	// "Defaults requiretty" in /etc/sudoers - I'm looking at you RedHat
	if err := session.RequestPty("xterm", termHeight, termWidth, modes); err != nil {
		return "", err
	}

	output, err := session.CombinedOutput(command)

	return string(bytes.TrimSpace(output)), wrapError(err)
}

// Start starts the specified command without waiting for it to finish. You
// have to call the Wait function for that.
func (client *Client) Start(command string) (io.ReadCloser, io.ReadCloser, error) {
	session, conn, err := client.newSession()
	if err != nil {
		return nil, nil, err
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		return nil, nil, nonil(err, session.Close(), conn.Close())
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return nil, nil, nonil(err, session.Close(), conn.Close())
	}
	if err := session.Start(command); err != nil {
		return nil, nil, nonil(err, session.Close(), conn.Close())
	}
	client.openSession = session
	client.openClient = conn
	return ioutil.NopCloser(stdout), ioutil.NopCloser(stderr), nil
}

// Wait waits for the command started by the Start function to exit. The
// returned error follows the same logic as in the exec.Cmd.Wait function.
func (client *Client) Wait() (err error) {
	if client.openSession != nil {
		err = client.openSession.Wait()
		_ = client.openSession.Close()
		client.openSession = nil
	}
	if client.openClient != nil {
		_ = client.openClient.Close()
		client.openClient = nil
	}
	return err
}

// Shell requests a shell from the remote. If an arg is passed, it tries to
// exec them on the server.
func (client *Client) Shell(args ...string) error {
	var (
		termWidth, termHeight = 80, 24
	)
	conn, err := ssh.Dial("tcp", client.Config.addr(), client.Config.native())
	if err != nil {
		return err
	}
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		return err
	}

	defer session.Close()

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr
	session.Stdin = os.Stdin

	modes := ssh.TerminalModes{
		ssh.ECHO: 1,
	}

	fd := os.Stdin.Fd()

	if term.IsTerminal(fd) {
		oldState, err := term.MakeRaw(fd)
		if err != nil {
			return err
		}

		defer term.RestoreTerminal(fd, oldState)

		winsize, err := term.GetWinsize(fd)
		if err == nil {
			termWidth = int(winsize.Width)
			termHeight = int(winsize.Height)
		}
	}

	if err := session.RequestPty("xterm", termHeight, termWidth, modes); err != nil {
		return err
	}

	if len(args) != 0 {
		session.Run(strings.Join(args, " "))
		return nil
	}

	if err := session.Shell(); err != nil {
		return err
	}
	// monitor for sigwinch
	go monWinCh(session, os.Stdout.Fd())
	session.Wait()
	return nil
}
