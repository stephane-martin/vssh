package commands

import (
	"context"
	"errors"
	"io/ioutil"
	"net"
	"strings"

	"github.com/stephane-martin/vssh/crypto"
	"github.com/stephane-martin/vssh/params"
	"github.com/stephane-martin/vssh/remoteops"
	"github.com/stephane-martin/vssh/sys"

	"github.com/getlantern/go-socks5"
	"github.com/getlantern/golog"
	"github.com/getlantern/hidden"
	gssh "github.com/stephane-martin/golang-ssh"
	"github.com/urfave/cli"
)

func SocksCommand() cli.Command {
	// TODO: support DNS on UDP
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

	_, credentials, err := crypto.GetSSHCredentials(ctx, c, sshParams.LoginName, sshParams.UseAgent, logger)
	if err != nil {
		return err
	}
	methods := crypto.CredentialsToMethods(credentials, logger)
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
	socksAddr := clictx.String("socksaddr")
	listener, err := net.Listen("tcp", socksAddr)
	if err != nil {
		return err
	}
	logger.Infow("SOCKS server listening", "addr", socksAddr)
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	return socksServer.Serve(listener)
}
