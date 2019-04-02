package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/awnumar/memguard"
	"go.uber.org/zap"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ktr0731/go-fuzzyfinder"
	vexec "github.com/stephane-martin/vault-exec/lib"
	"github.com/stephane-martin/vssh/lib"
	"github.com/urfave/cli"
)

const folderIcon = "\xF0\x9F\x97\x80 "
const fileIcon = "\xF0\x9F\x97\x88 "

func scpPutCommand() cli.Command {
	return cli.Command{
		Name:  "put",
		Usage: "upload files with scp using Vault for authentication",
		Flags: []cli.Flag{
			cli.StringSliceFlag{
				Name:  "source",
				Usage: "file to copy",
			},
			cli.StringFlag{
				Name:  "destination,dest,dst",
				Usage: "file path on the remote server",
				Value: ".",
			},
		},
		Action: wrapPut(lib.ScpPut),
	}
}

func sftpPutCommand() cli.Command {
	return cli.Command{
		Name:  "put",
		Usage: "upload files with SFTP using Vault for authentication",
		Flags: []cli.Flag{
			cli.StringSliceFlag{
				Name:  "source",
				Usage: "file to copy",
			},
			cli.StringFlag{
				Name:  "destination,dest,dst",
				Usage: "file path on the remote server",
				Value: ".",
			},
		},
		Action: wrapPut(lib.SFTPPut),
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

type putFunc func(context.Context, []lib.Source, string, lib.SSHParams, *memguard.LockedBuffer, *memguard.LockedBuffer, *zap.SugaredLogger) error

func wrapPut(f putFunc) cli.ActionFunc {
	return func(c *cli.Context) (e error) {
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

		sourcesNames := transform(c.StringSlice("source"))
		if len(sourcesNames) == 0 {
			paths, err := lib.Walk()
			if err != nil {
				return err
			}
			idx, _ := fuzzyfinder.FindMulti(paths, func(i int) string {
				if paths[i].IsDir {
					return folderIcon + paths[i].RelName
				}
				return fileIcon + paths[i].RelName
			})
			for _, i := range idx {
				sourcesNames = append(sourcesNames, paths[i].Path)
			}
			if len(sourcesNames) == 0 {
				return errors.New("you must specify the sources")
			}
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
		_ = os.Unsetenv("VAULT_ADDR")

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
		return f(ctx, sources, dest, sshParams, privkey, signed, logger)
	}
}
