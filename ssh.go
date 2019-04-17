package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"syscall"

	vexec "github.com/stephane-martin/vault-exec/lib"
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
			cli.StringFlag{
				Name:   "login,l",
				Usage:  "SSH remote user",
				EnvVar: "SSH_USER",
			},
			cli.IntFlag{
				Name:   "ssh-port,sshport,p",
				Usage:  "SSH remote port",
				EnvVar: "SSH_PORT",
				Value:  22,
			},
			cli.StringFlag{
				Name:   "privkey,private,identity,i",
				Usage:  "filesystem path to SSH private key",
				EnvVar: "IDENTITY",
				Value:  "",
			},
			cli.StringFlag{
				Name:   "vprivkey,vprivate,videntity",
				Usage:  "Vault secret path to SSH private key",
				EnvVar: "VIDENTITY",
				Value:  "",
			},
			cli.BoolFlag{
				Name:   "insecure",
				Usage:  "do not check the remote SSH host key",
				EnvVar: "SSH_INSECURE",
			},
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

func sshAction(c *cli.Context) (e error) {
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
		LogLevel: strings.ToLower(strings.TrimSpace(c.GlobalString("loglevel"))),
		Prefix:   c.Bool("prefix"),
		Upcase:   c.Bool("upcase"),
	}

	logger, err := vexec.Logger(params.LogLevel)
	if err != nil {
		return err
	}
	defer func() { _ = logger.Sync() }()

	args := c.Args()
	if len(args) == 0 {
		return errors.New("no host provided")
	}
	sshParams, err := getSSHParams(c, params.LogLevel == DEBUG, args)
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

	secretPaths := c.StringSlice("secret")
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

	// TODO: restore native connext
	return lib.GoConnectAuth(ctx, sshParams, methods, secrets, logger)
}
