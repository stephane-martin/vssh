package main

import (
	"context"
	"errors"
	"io/ioutil"
	"net"
	"strings"

	"github.com/getlantern/go-socks5"
	"github.com/getlantern/golog"
	"github.com/getlantern/hidden"
	gssh "github.com/stephane-martin/golang-ssh"
	"github.com/stephane-martin/vssh/lib"
	"github.com/urfave/cli"
	"golang.org/x/crypto/ssh"
)

func socksCommand() cli.Command {
	return cli.Command{
		Name:   "socks",
		Action: socksAction,
		Usage:  "starts a SOCKS5 server to forward connections to a remote SSH server",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "dnsaddr",
				Usage: "DNS server address on the remote side (optional, ex: 127.0.0.1:53)",
			},
			cli.StringFlag{
				Name:  "socksaddr",
				Usage: "SOCKS listen address",
				Value: "127.0.0.1:1180",
			},
		},
	}
}

func socksAction(clictx *cli.Context) (e error) {
	defer func() {
		if e != nil {
			e = cli.NewExitError(e.Error(), 1)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelOnSignal(cancel)

	params := lib.Params{
		LogLevel: strings.ToLower(strings.TrimSpace(clictx.GlobalString("loglevel"))),
	}

	logger, err := Logger(params.LogLevel)
	if err != nil {
		return err
	}
	defer func() { _ = logger.Sync() }()

	var c CLIContext = cliContext{ctx: clictx}
	if c.SSHHost() == "" {
		var err error
		c, err = Form(c, true)
		if err != nil {
			return err
		}
	}

	sshParams, err := getSSHParams(c)
	if err != nil {
		return err
	}

	_, credentials, err := getCredentials(ctx, c, sshParams.LoginName, logger)
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
		User: sshParams.LoginName,
		Host: sshParams.Host,
		Port: sshParams.Port,
		Auth: methods,
	}
	hkcb, err := gssh.MakeHostKeyCallback(sshParams.Insecure, logger)
	if err != nil {
		return err
	}
	cfg.HostKey = hkcb
	client, err := gssh.Dial(cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	dnsServer := clictx.String("dnsaddr")
	if dnsServer == "" {
		dnsServers, err := lib.FindDNSServers(client)
		if err != nil {
			return err
		}
		if len(dnsServers) == 0 {
			return errors.New("no DNS server found in /etc/resolv.conf")
		}
		dnsServer = dnsServers[0] + ":53"
		logger.Debugw("discovered DNS server in /etc/resolv.conf", "addr", dnsServer)
	}
	resolver := lib.NewResolver(client, dnsServer, logger)

	socksConfig := socks5.Config{
		Resolver: resolver,
		Dial: func(_ context.Context, network, addr string) (net.Conn, error) {
			return client.Dial(network, addr)
		},
	}
	golog.SetOutputs(ioutil.Discard, ioutil.Discard)
	golog.RegisterReporter(func(err error, linePrefix string, severity golog.Severity, ctx map[string]interface{}) {
		kv := make([]interface{}, 0, 2*len(ctx)+2)
		kv = append(kv, "error", hidden.Clean(err.Error()))
		for k, v := range ctx {
			kv = append(kv, k, v)
		}
		logger.Debugw("socks error", kv...)
	})

	socksServer, err := socks5.New(&socksConfig)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", clictx.String("socksaddr"))
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	return socksServer.Serve(listener)

}
