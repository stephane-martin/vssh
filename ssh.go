package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/stephane-martin/vssh/lib"
	"github.com/urfave/cli"
	"golang.org/x/crypto/ssh"
)

const (
	DEBUG = "debug"
)

func sshCommand() cli.Command {
	return cli.Command{
		Name:   "ssh",
		Usage:  "connect to remote server with SSH using Vault for authentication",
		Action: sshAction,
		Flags: []cli.Flag{
			cli.BoolFlag{
				Name:   "native",
				Usage:  "use the native SSH client instead of the builtin one",
				EnvVar: "SSH_NATIVE",
			},
			cli.BoolFlag{
				Name:   "terminal,t",
				Usage:  "force pseudo-terminal allocation",
				EnvVar: "SSH_FORCE_PSEUDO",
			},
			cli.StringSliceFlag{
				Name:  "secret,key",
				Usage: "path of a secret to be read from Vault (multiple times)",
			},
			cli.BoolFlag{
				Name:   "upcase,up",
				Usage:  "convert all environment variable keys to uppercase",
				EnvVar: "UPCASE",
			},
			cli.BoolFlag{
				Name:   "prefix",
				Usage:  "prefix the environment variable keys with names of secrets",
				EnvVar: "PREFIX",
			},
		},
	}
}

func sshAction(clictx *cli.Context) (e error) {
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
		Prefix:   clictx.Bool("prefix"),
		Upcase:   clictx.Bool("upcase"),
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

	client, credentials, err := getCredentials(ctx, c, sshParams.LoginName, logger)
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

	secretPaths := clictx.StringSlice("secret")
	var secrets map[string]string
	if len(secretPaths) > 0 {
		if client != nil {
			res, err := lib.GetSecretsFromVault(ctx, client, secretPaths, params.Prefix, params.Upcase, logger)
			if err != nil {
				return err
			}
			secrets = res
		} else {
			logger.Warnw("can't read secrets from vault: no vault client")
		}
	}

	// TODO: restore native connect
	return lib.GoConnectAuth(ctx, sshParams, c.ForceTerminal(), methods, secrets, logger)
}
