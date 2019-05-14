package commands

import (
	"context"
	"errors"
	"fmt"
	"github.com/stephane-martin/vssh/crypto"
	"github.com/stephane-martin/vssh/params"
	"github.com/stephane-martin/vssh/remoteops"
	"github.com/stephane-martin/vssh/sys"
	"github.com/stephane-martin/vssh/widgets"
	"strings"

	gssh "github.com/stephane-martin/golang-ssh"
	"github.com/urfave/cli"
	"golang.org/x/crypto/ssh"
)

func ResolveCommand() cli.Command {
	return cli.Command{
		Name:   "resolve",
		Action: resolveAction,
		Usage:  "resolve a hostname through a SSH connection",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "addr",
				Usage: "DNS server address on the remote side",
			},
			cli.StringFlag{
				Name:  "hostname",
				Usage: "the hostname to resolve",
			},
		},
	}
}

func resolveAction(clictx *cli.Context) (e error) {
	defer func() {
		if e != nil {
			e = cli.NewExitError(e.Error(), 1)
		}
	}()

	hostname := clictx.String("hostname")
	if hostname == "" {
		return errors.New("specify the hostname to solve")
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
		var err error
		c, err = widgets.Form(c, true)
		if err != nil {
			return err
		}
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
	dnsServer := clictx.String("addr")
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
	_, ip, err := resolver.Resolve(context.Background(), hostname)
	if err != nil {
		return fmt.Errorf("failed to resolve %s: %s", hostname, err)
	}
	fmt.Println(ip.String())
	return nil
}
