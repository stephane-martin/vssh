package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"

	vexec "github.com/stephane-martin/vault-exec/lib"
	"github.com/stephane-martin/vssh/lib"
	"github.com/urfave/cli"
)

func scpCommand() cli.Command {
	return cli.Command{
		Name:  "scp",
		Usage: "scp using Vault for authentication",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:   "login_name,ssh-user,l",
				Usage:  "SSH remote user",
				EnvVar: "SSH_USER",
			},
			cli.IntFlag{
				Name:   "ssh-port,p",
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
				EnvVar: "VSSH_INSECURE",
			},
			cli.StringFlag{
				Name:  "source,src",
				Usage: "file to copy",
			},
			cli.StringFlag{
				Name:  "destination,dest,dst",
				Usage: "file path on the remote server",
			},
		},
		Action: scpAction,
	}
}

func scpAction(c *cli.Context) (e error) {
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
			fmt.Fprintln(os.Stderr, "signal!")
			cancel()
		}
	}()

	sourceFname := strings.TrimSpace(c.String("source"))
	if sourceFname == "" {
		return errors.New("you must specify the source")
	}
	source, err := lib.FileSource(sourceFname)
	if err != nil {
		return fmt.Errorf("error reading source: %s", err)
	}
	defer source.Close()
	dest := strings.TrimSpace(c.String("destination"))
	if dest == "" {
		dest = path.Base(sourceFname)
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
	sshParams, err := GetSSHParams(c, params.LogLevel == "debug", args)
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

	signed, err := lib.Sign(ctx, pubkey, sshParams.LoginName, vaultParams, client)
	if err != nil {
		return fmt.Errorf("signing error: %s", err)
	}
	return lib.GoSCP(ctx, source, dest, sshParams, privkey, signed, logger)
}