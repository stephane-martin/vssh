package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	vexec "github.com/stephane-martin/vault-exec/lib"
	"github.com/stephane-martin/vssh/lib"
	"github.com/urfave/cli"
)

func uploadCommand() cli.Command {
	return cli.Command{
		Name:  "upload",
		Usage: "upload files with scp using Vault for authentication",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:   "login,l",
				Usage:  "SSH remote user",
				EnvVar: "SSH_USER",
			},
			cli.IntFlag{
				Name:   "ssh-port,sshport,P",
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
			cli.StringSliceFlag{
				Name:  "source",
				Usage: "file to copy",
			},
			cli.StringFlag{
				Name:  "destination,dest,dst",
				Usage: "file path on the remote server",
			},
		},
		Action: uploadAction,
	}
}

func transform(a []string) []string {
	var b []string
	for _, s := range a {
		s = strings.TrimSpace(s)
		if s != "" {
			b = append(b, s)
		}
	}
	return b
}

func uploadAction(c *cli.Context) (e error) {
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
			//fmt.Fprintln(os.Stderr, "signal!")
			cancel()
		}
	}()

	sourcesNames := transform(c.StringSlice("source"))
	if len(sourcesNames) == 0 {
		return errors.New("you must specify the sources")
	}
	sources := make([]lib.Source, 0, len(sourcesNames))
	for _, name := range sourcesNames {
		s, err := lib.MakeSource(name)
		if err != nil {
			return fmt.Errorf("error reading source %s: %s", name, err)
		}
		sources = append(sources, s)
	}

	dest := strings.TrimSpace(c.String("destination"))
	if dest == "" {
		dest = "."
	}

	vaultParams := lib.GetVaultParams(c)
	if vaultParams.SSHMount == "" {
		return errors.New("empty SSH mount point")
	}
	if vaultParams.SSHRole == "" {
		return errors.New("empty SSH role")
	}

	params := lib.Params{
		LogLevel: strings.ToLower(strings.TrimSpace(c.GlobalString("loglevel"))),
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

	// unset env VAULT_ADDR to prevent the vault client from seeing it
	os.Unsetenv("VAULT_ADDR")

	client, err := vexec.Auth(
		ctx,
		vaultParams.AuthMethod,
		vaultParams.Address,
		vaultParams.AuthPath,
		vaultParams.Token,
		vaultParams.Username,
		vaultParams.Password,
		logger,
	)
	if err != nil {
		return fmt.Errorf("auth failed: %s", err)
	}
	err = vexec.CheckHealth(ctx, client)
	if err != nil {
		return fmt.Errorf("Vault health check error: %s", err)
	}

	privkey, err := lib.ReadPrivateKey(ctx, c.String("privkey"), c.String("vprivkey"), client, logger)
	if err != nil {
		return fmt.Errorf("failed to read private key: %s", err)
	}
	pubkey, err := lib.DerivePublicKey(privkey)
	if err != nil {
		return fmt.Errorf("error extracting public key: %s", err)
	}

	signed, err := lib.Sign(ctx, pubkey, sshParams.LoginName, vaultParams, client, logger)
	if err != nil {
		return fmt.Errorf("signing error: %s", err)
	}
	return lib.Upload(ctx, sources, dest, sshParams, privkey, signed, logger)
}
