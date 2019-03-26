package main

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	vexec "github.com/stephane-martin/vault-exec/lib"
	"github.com/stephane-martin/vssh/lib"
	"github.com/urfave/cli"
	"go.uber.org/zap"
)

func downloadCommand() cli.Command {
	return cli.Command{
		Name:  "download",
		Usage: "download files with scp using Vault for authentication",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:   "login_name,ssh-user,l",
				Usage:  "SSH remote user",
				EnvVar: "SSH_USER",
			},
			cli.IntFlag{
				Name:   "ssh-port,P",
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
			cli.StringSliceFlag{
				Name:  "source,src",
				Usage: "file to copy on the remote server",
			},
			cli.StringFlag{
				Name:  "destination,dest,dst",
				Usage: "local file path",
			},
			cli.BoolFlag{
				Name:  "preserve,p",
				Usage: "preserves modification times, access times, and modes from the original file",
			},
		},
		Action: downloadAction,
	}
}

func downloadAction(c *cli.Context) (e error) {
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

	sources := transform(c.StringSlice("source"))
	if len(sources) == 0 {
		return errors.New("you must specify the sources")
	}

	dest := strings.TrimSpace(c.String("destination"))
	if dest == "" {
		dest = "."
	}
	stats, err := os.Stat(dest)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat error on destination: %s", err)
	}
	destExists := err == nil
	destIsDir := err == nil && stats.IsDir()
	if len(sources) > 1 && destExists && !destIsDir {
		return errors.New("multiple copies but destination is not a directory")
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
	return lib.Download(ctx, sources, sshParams, privkey, signed, makeCB(dest, c.Bool("preserve"), logger), logger)
}

func makeCB(dest string, preserve bool, l *zap.SugaredLogger) lib.Callback {
	return func(isDir, endOfDir bool, name string, perms os.FileMode, mtime time.Time, atime time.Time, content []byte) error {
		path := filepath.Join(dest, name)

		if endOfDir && preserve {
			l.Debugw("end of directory", "name", name)
			err := os.Chmod(path, perms.Perm())
			if err != nil {
				l.Infow("failed to chmod directory", "name", path, "error", err)
			}
			err = os.Chtimes(path, atime, mtime)
			if err != nil {
				l.Infow("failed to chtimes directory", "name", path, "error", err)
			}
			return nil
		}

		if isDir {
			l.Debugw("received directory", "name", name)
			stats, err := os.Stat(path)
			if err != nil {
				if os.IsNotExist(err) {
					err := os.MkdirAll(path, 0700)
					if err != nil {
						return fmt.Errorf("failed to create directory %s: %s", path, err)
					}
				} else {
					return fmt.Errorf("failed to stat %s: %s", path, err)
				}
			} else {
				if stats.IsDir() {
					err := os.Chmod(path, stats.Mode().Perm()|0700)
					if err != nil {
						l.Infow("failed to chmod directory", "name", path, "error", err)
					}
				} else {
					return fmt.Errorf("file already exists and is not a directory: %s", path)
				}
			}
			return nil
		}

		l.Debugw("received file", "name", name, "size", len(content))
		err := ioutil.WriteFile(path, content, perms.Perm()|0600)
		if err != nil {
			return fmt.Errorf("failed to write file %s: %s", path, err)
		}
		if preserve {
			err := os.Chmod(path, perms.Perm())
			if err != nil {
				l.Infow("failed to chmod file", "name", path, "error", err)
			}
			err = os.Chtimes(path, atime, mtime)
			if err != nil {
				l.Infow("failed to chtimes file", "name", path, "error", err)
			}
		}
		return nil
	}
}
