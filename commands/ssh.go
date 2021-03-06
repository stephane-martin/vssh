package commands

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/stephane-martin/vssh/crypto"
	"github.com/stephane-martin/vssh/params"
	"github.com/stephane-martin/vssh/vault"
	"github.com/stephane-martin/vssh/widgets"

	"github.com/stephane-martin/vssh/lib"
	"github.com/urfave/cli"
)

const (
	DEBUG = "debug"
)

func SSHCommand() cli.Command {
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

	gparams := params.Params{
		LogLevel: strings.ToLower(strings.TrimSpace(clictx.GlobalString("loglevel"))),
		Prefix:   clictx.Bool("prefix"),
		Upcase:   clictx.Bool("upcase"),
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

	client, credentials, err := crypto.GetSSHCredentials(ctx, c, sshParams.LoginName, sshParams.UseAgent, logger)
	if err != nil {
		return err
	}

	methods := crypto.CredentialsToMethods(credentials, logger)
	if len(methods) == 0 {
		return errors.New("no usable credentials")
	}

	secretPaths := clictx.StringSlice("secret")
	var secrets map[string]string
	if len(secretPaths) > 0 {
		if client == nil {
			return errors.New("can't read secrets from vault: no vault client")
		}
		res, err := vault.GetSecretsFromVault(ctx, client, secretPaths, gparams.Prefix, gparams.Upcase, logger)
		if err != nil {
			return err
		}
		secrets = res
	}

	// TODO: restore native connect
	return lib.GoConnectAuth(ctx, sshParams, c.ForceTerminal(), methods, secrets, logger)
}
