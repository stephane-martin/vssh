package commands

import (
	"context"
	"errors"
	"github.com/stephane-martin/vssh/crypto"
	"github.com/stephane-martin/vssh/params"
	"github.com/stephane-martin/vssh/sys"
	"io"
	"net"
	"strings"

	gssh "github.com/stephane-martin/golang-ssh"
	"github.com/urfave/cli"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sync/errgroup"
)

func TunnelCommand() cli.Command {
	return cli.Command{
		Name:  "tunnel",
		Usage: "make a local or remote SSH tunnel",
		Subcommands: []cli.Command{
			{
				Name:   "local",
				Usage:  "make a local SSH tunnel",
				Action: localTunnelAction,
				Flags: []cli.Flag{
					cli.StringFlag{
						Name:  "local-addr,local",
						Usage: "local listen address",
					},
					cli.StringFlag{
						Name:  "remote-addr,remote",
						Usage: "remote connection address",
					},
				},
			},
			{
				Name:   "remote",
				Usage:  "make a remote SSH tunnel",
				Action: remoteTunnelAction,
				Flags: []cli.Flag{
					cli.StringFlag{
						Name:  "local-addr,local",
						Usage: "local connection address",
					},
					cli.StringFlag{
						Name:  "remote-addr,remote",
						Usage: "remote listen address",
					},
				},
			},
		},
	}
}

func localTunnelAction(clictx *cli.Context) (e error) {
	defer func() {
		if e != nil {
			e = cli.NewExitError(e.Error(), 1)
		}
	}()

	local := clictx.String("local")
	remote := clictx.String("remote")
	if local == "" {
		return errors.New("specify local listen address")
	}
	if remote == "" {
		return errors.New("specify remote connection address")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sys.CancelOnSignal(cancel)

	gparams := params.Params{
		LogLevel: strings.ToLower(strings.TrimSpace(clictx.GlobalString("loglevel"))),
	}

	logger, err := params.Logger(gparams.LogLevel)
	if err != nil {
		return err
	}
	defer func() { _ = logger.Sync() }()

	c := params.NewCliContext(clictx)
	if c.SSHHost() == "" {
		return errors.New("specify SSH host")
	}

	sshParams, err := params.GetSSHParams(c)
	if err != nil {
		return err
	}

	_, credentials, err := crypto.GetSSHCredentials(ctx, c, sshParams.LoginName, logger)
	if err != nil {
		return err
	}

	var methods []ssh.AuthMethod
	for _, credential := range credentials {
		m, err := credential.AuthMethod()
		if err == nil {
			methods = append(methods, m)
		} else {
			logger.Errorw("failed to use credentials", "error", err)
		}
	}
	if len(methods) == 0 {
		return errors.New("no usable credentials")
	}

	cfg := gssh.Config{
		User:      sshParams.LoginName,
		Host:      sshParams.Host,
		Port:      sshParams.Port,
		Auth:      methods,
		HTTPProxy: sshParams.HTTPProxy,
	}
	hkcb, err := gssh.MakeHostKeyCallback(sshParams.Insecure, logger)
	if err != nil {
		return err
	}
	cfg.HostKey = hkcb
	client, err := gssh.Dial(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	listener, err := net.Listen("tcp", local)
	if err != nil {
		return err
	}
	defer listener.Close()
	logger.Infow("listening on local address", "address", local)
	g, lctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		<-lctx.Done()
		return listener.Close()
	})

	g.Go(func() error {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return err
			}
			logger.Infow("accepted a new local connection", "client", conn.RemoteAddr().String())
			g.Go(func() error {
				<-lctx.Done()
				return conn.Close()
			})
			g.Go(func() error {
				remoteConn, err := client.Dial("tcp", remote)
				if err != nil {
					logger.Warnw("failed to dial the remote service", "error", err)
					_ = conn.Close()
					return nil
				}
				logger.Debug("successfully opened a connection to the remote side")
				_ = handleLocalConn(conn, remoteConn)
				logger.Infow("closed local connection", "client", conn.RemoteAddr().String())
				return nil
			})
		}
	})

	err = g.Wait()
	if err == context.Canceled {
		return nil
	}
	return err
}

func handleLocalConn(localConn net.Conn, remoteConn net.Conn) error {
	go func() {
		// copy what the server sends to the local socket
		_, _ = io.Copy(localConn, remoteConn)
		_ = localConn.Close()
	}()
	// copy the incoming local data to the remote server
	_, _ = io.Copy(remoteConn, localConn)
	_ = remoteConn.Close()
	return nil
}

func remoteTunnelAction(clictx *cli.Context) (e error) {
	defer func() {
		if e != nil {
			e = cli.NewExitError(e.Error(), 1)
		}
	}()

	local := clictx.String("local")
	remote := clictx.String("remote")
	if local == "" {
		return errors.New("specify local connection address")
	}
	if remote == "" {
		return errors.New("specify remote listen address")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sys.CancelOnSignal(cancel)

	gparams := params.Params{
		LogLevel: strings.ToLower(strings.TrimSpace(clictx.GlobalString("loglevel"))),
	}

	logger, err := params.Logger(gparams.LogLevel)
	if err != nil {
		return err
	}
	defer func() { _ = logger.Sync() }()

	c := params.NewCliContext(clictx)
	if c.SSHHost() == "" {
		return errors.New("specify SSH host")
	}

	sshParams, err := params.GetSSHParams(c)
	if err != nil {
		return err
	}

	_, credentials, err := crypto.GetSSHCredentials(ctx, c, sshParams.LoginName, logger)
	if err != nil {
		return err
	}

	var methods []ssh.AuthMethod
	for _, credential := range credentials {
		m, err := credential.AuthMethod()
		if err == nil {
			methods = append(methods, m)
		} else {
			logger.Errorw("failed to use credentials", "error", err)
		}
	}
	if len(methods) == 0 {
		return errors.New("no usable credentials")
	}

	cfg := gssh.Config{
		User:      sshParams.LoginName,
		Host:      sshParams.Host,
		Port:      sshParams.Port,
		Auth:      methods,
		HTTPProxy: sshParams.HTTPProxy,
	}
	hkcb, err := gssh.MakeHostKeyCallback(sshParams.Insecure, logger)
	if err != nil {
		return err
	}
	cfg.HostKey = hkcb
	client, err := gssh.Dial(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	listener, err := client.Listen("tcp", remote)
	if err != nil {
		return err
	}
	defer listener.Close()
	logger.Infow("listening on remote address", "address", remote)
	g, lctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		<-lctx.Done()
		return listener.Close()
	})

	g.Go(func() error {
		for {
			remoteConn, err := listener.Accept()
			if err != nil {
				return err
			}
			logger.Infow("accepted a new remote connection", "client", remoteConn.RemoteAddr().String())
			g.Go(func() error {
				<-lctx.Done()
				return remoteConn.Close()
			})
			g.Go(func() error {
				localConn, err := net.Dial("tcp", local)
				if err != nil {
					logger.Warnw("failed to dial the local service", "error", err)
					_ = remoteConn.Close()
					return nil
				}
				logger.Debug("successfully opened a connection to the local side")
				_ = handleRemoteConn(localConn, remoteConn)
				logger.Infow("closed remote connection", "client", remoteConn.RemoteAddr().String())
				return nil
			})
		}
	})

	err = g.Wait()
	if err == context.Canceled {
		return nil
	}
	return err
}

func handleRemoteConn(localConn net.Conn, remoteConn net.Conn) error {
	go func() {
		// copy back
		_, _ = io.Copy(remoteConn, localConn)
		_ = remoteConn.Close()
	}()
	// copy the incoming local data to the remote server
	_, _ = io.Copy(localConn, remoteConn)
	_ = localConn.Close()
	return nil
}
