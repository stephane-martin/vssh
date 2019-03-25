package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"strings"
	"syscall"

	vexec "github.com/stephane-martin/vault-exec/lib"
	"github.com/stephane-martin/vssh/lib"
	"github.com/urfave/cli"
)

func sshCommand() cli.Command {
	return cli.Command{
		Name:   "ssh",
		Usage:  "SSH to remote server",
		Action: sshAction,
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
			cli.BoolFlag{
				Name:   "native",
				Usage:  "use the native SSH client instead of the builtin one",
				EnvVar: "VSSH_NATIVE",
			},
			cli.BoolFlag{
				Name:   "t",
				Usage:  "force pseudo-terminal allocation",
				EnvVar: "VSSH_FORCE_PSEUDO",
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

	vaultParams := lib.GetVaultParams(c)
	if vaultParams.SSHMount == "" {
		return errors.New("empty SSH mount point")
	}
	if vaultParams.SSHRole == "" {
		return errors.New("empty SSH role")
	}

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

	secretPaths := c.StringSlice("secret")
	var secrets map[string]string
	if len(secretPaths) > 0 {
		res, err := lib.GetSecretsFromVault(ctx, client, secretPaths, params.Prefix, params.Upcase, logger)
		if err != nil {
			return err
		}
		secrets = res
	}

	privkey, err := lib.ReadPrivateKey(ctx, c.String("privkey"), c.String("vprivkey"), client, logger)
	if err != nil {
		return fmt.Errorf("failed to read private key: %s", err)
	}
	pubkey, err := lib.DerivePublicKey(privkey)
	if err != nil {
		return fmt.Errorf("error extracting public key: %s", err)
	}

	signed, err := lib.Sign(pubkey, sshParams.LoginName, vaultParams, client)
	if err != nil {
		return fmt.Errorf("signing error: %s", err)
	}

	return lib.Connect(ctx, sshParams, privkey, pubkey, signed, secrets, logger)
}

func GetSSHParams(c *cli.Context, verbose bool, args []string) (p lib.SSHParams, err error) {
	p.Verbose = verbose
	p.Host = strings.TrimSpace(args[0])
	if p.Host == "" {
		return p, errors.New("empty host")
	}
	spl := strings.SplitN(p.Host, "@", 2)
	if len(spl) == 2 {
		p.LoginName = spl[0]
		p.Host = spl[1]
	}
	if p.LoginName == "" {
		p.LoginName = c.String("login_name")
		if p.LoginName == "" {
			u, err := user.Current()
			if err != nil {
				return p, err
			}
			p.LoginName = u.Username
		}
	}
	p.Commands = args[1:]

	p.Insecure = c.Bool("insecure")
	p.Port = c.Int("ssh-port")
	p.Native = c.Bool("native")
	p.ForceTerminal = c.Bool("t")

	return p, nil
}
