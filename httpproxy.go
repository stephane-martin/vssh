package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/stephane-martin/vssh/crypto"
	"github.com/stephane-martin/vssh/params"
	"github.com/stephane-martin/vssh/remoteops"
	"github.com/stephane-martin/vssh/sys"
	"net"
	"net/http"
	"strings"

	"github.com/elazarl/goproxy"
	gssh "github.com/stephane-martin/golang-ssh"
	"github.com/urfave/cli"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

func httpProxyCommand() cli.Command {
	return cli.Command{
		Name:   "httpproxy",
		Action: httpProxyAction,
		Usage:  "starts a HTTP proxy to forward HTTP requests to remote SSH server",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "dnsaddr",
				Usage: "DNS server address on the remote side (optional, ex: 127.0.0.1:53)",
			},
			cli.StringFlag{
				Name:  "httpaddr",
				Usage: "HTTP proxy listen address",
				Value: "127.0.0.1:8080",
			},
		},
	}
}

func httpProxyAction(clictx *cli.Context) (e error) {
	defer func() {
		if e != nil {
			e = cli.NewExitError(e.Error(), 1)
		}
	}()

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
	defer func() { _ = client.Close() }()

	dnsServer := clictx.String("dnsaddr")
	if dnsServer == "" {
		dnsServers, err := remoteops.FindDNSServers(client)
		if err != nil {
			return err
		}
		if len(dnsServers) == 0 {
			return errors.New("no DNS server found in /etc/resolv.conf")
		}
		dnsServer = dnsServers[0] + ":53"
		logger.Debugw("discovered DNS server in /etc/resolv.conf", "addr", dnsServer)
	}
	resolver := remoteops.NewResolver(client, dnsServer, logger)

	listener, err := net.Listen("tcp", clictx.String("httpaddr"))
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = true
	dial := func(network string, addr string) (net.Conn, error) {
		h, p, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		_, ipAddr, err := resolver.Resolve(context.Background(), h)
		if err != nil {
			return nil, err
		}
		return client.Dial("tcp", net.JoinHostPort(ipAddr.String(), p))
	}
	proxy.Logger = proxyLogger{z: logger}
	proxy.ConnectDial = dial
	proxy.Tr = &http.Transport{
		Dial:               dial,
		DisableCompression: true,
	}

	return http.Serve(listener, proxy)
}

type proxyLogger struct {
	z *zap.SugaredLogger
}

func (l proxyLogger) Printf(format string, v ...interface{}) {
	s := strings.TrimSpace(fmt.Sprintf(format, v...))
	if strings.HasPrefix(s, "[") {
		spl := strings.SplitN(s, "]", 2)
		if len(spl) == 1 {
			l.z.Debug(s)
			return
		}
		l.z.Debugw(strings.TrimSpace(spl[1]), "session", spl[0][1:])
		return
	}
	l.z.Debug(s)
}
