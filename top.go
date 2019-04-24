package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	gssh "github.com/stephane-martin/golang-ssh"
	"github.com/stephane-martin/vssh/lib"
	"github.com/urfave/cli"
	"golang.org/x/crypto/ssh"
)

func topCommand() cli.Command {
	return cli.Command{
		Name:   "top",
		Action: topAction,
	}
}

func topAction(clictx *cli.Context) (e error) {
	defer func() {
		if e != nil {
			e = cli.NewExitError(e.Error(), 1)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for range sigchan {
			cancel()
		}
	}()

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
		if c.SSHHost() == "" {
			var err error
			c, err = Form(c, true)
			if err != nil {
				return err
			}
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
	stater := NewStater(client)
	for {
		stats, err := stater.Get()
		if err != nil {
			return err
		}
		fmt.Printf("%+v\n", stats)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}

}
