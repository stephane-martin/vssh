package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/awnumar/memguard"
	"io"
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

func scpGetCommand() cli.Command {
	return cli.Command{
		Name:  "get",
		Usage: "download files with scp using Vault for authentication",
		Flags: []cli.Flag{
			cli.StringSliceFlag{
				Name:  "target",
				Usage: "file to copy from the remote server",
			},
			cli.StringFlag{
				Name:  "destination,dest,dst",
				Usage: "local file path",
				Value: ".",
			},
			cli.BoolFlag{
				Name:  "preserve,p",
				Usage: "preserves modification times, access times, and modes from the original file",
			},
		},
		Action: wrapGet(lib.ScpGet),
	}
}

func sftpGetCommand() cli.Command {
	return cli.Command{
		Name:  "get",
		Usage: "download files with scp using Vault for authentication",
		Flags: []cli.Flag{
			cli.StringSliceFlag{
				Name:  "target",
				Usage: "file to copy from the remote server",
			},
			cli.StringFlag{
				Name:  "destination,dest,dst",
				Usage: "local file path",
				Value: ".",
			},
			cli.BoolFlag{
				Name:  "preserve,p",
				Usage: "preserves modification times, access times, and modes from the original file",
			},
		},
		Action: wrapGet(lib.SFTPGet),
	}
}

type getFunc func(context.Context, []string, lib.SSHParams, *memguard.LockedBuffer, *memguard.LockedBuffer, lib.Callback, *zap.SugaredLogger) error

func wrapGet(f getFunc) cli.ActionFunc {
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

		sources := transform(c.StringSlice("target"))
		if len(sources) == 0 {
			// TODO: fuzzy finder
			return errors.New("you must specify the targets")
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
			return fmt.Errorf("not a directory: %s", dest)
		}
		if len(sources) > 1 && !destExists {
			return fmt.Errorf("no such file or directory: %s", dest)
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
		return f(
			ctx,
			sources,
			sshParams,
			privkey,
			signed,
			makeCB(
				dest,
				c.Bool("preserve"),
				destExists,
				destIsDir,
				logger,
			),
			logger,
		)
	}
}

var pathSeparator = string([]byte{os.PathSeparator})

func makeCB(dest string, preserve, destExists, destIsDir bool, l *zap.SugaredLogger) lib.Callback {
	return func(isDir, endOfDir bool, name string, perms os.FileMode, mtime time.Time, atime time.Time, content io.Reader) error {
		if endOfDir {
			// leave directory
			l.Debugw("end of directory", "name", name)
			if preserve {
				var path string
				if destIsDir {
					path = filepath.Join(dest, name)
				} else if destExists {
					return fmt.Errorf("not a directory: %s", dest)
				} else {
					// len(sources) is 1
					spl := strings.Split(name, pathSeparator)
					temp := []string{dest}
					temp = append(temp, spl[1:]...)
					path = filepath.Join(temp...)
				}
				err := os.Chmod(path, perms.Perm())
				if err != nil {
					l.Infow("failed to chmod directory", "name", path, "error", err)
				}
				err = os.Chtimes(path, atime, mtime)
				if err != nil {
					l.Infow("failed to chtimes directory", "name", path, "error", err)
				}
			}
			return nil
		} else if isDir {
			// enter directory
			var path string
			if destIsDir {
				path = filepath.Join(dest, name)
			} else if destExists {
				return fmt.Errorf("not a directory: %s", dest)
			} else {
				// len(sources) is 1
				spl := strings.Split(name, pathSeparator)
				temp := []string{dest}
				temp = append(temp, spl[1:]...)
				path = filepath.Join(temp...)
			}
			l.Debugw("received directory", "name", name, "writeto", path)
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
					return fmt.Errorf("not a directory: %s", path)
				}
			}
			return nil
		} else {
			// file
			var path string
			if destIsDir {
				path = filepath.Join(dest, name)
			} else if destExists {
				// destination exists but is not a directory
				// that means len(sources) is 1
				// so the operation is just a file copy
				path = dest
			} else {
				// destination does not exist
				// len(sources) is 1
				spl := strings.Split(name, pathSeparator)
				temp := []string{dest}
				temp = append(temp, spl[1:]...)
				path = filepath.Join(temp...)
			}

			l.Debugw("received file", "name", name, "writeto", path)
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perms.Perm()|0600)
			if err != nil {
				return fmt.Errorf("failed to open file %s: %s", path, err)
			}
			_, err = io.Copy(f, content)
			_ = f.Close()
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
}
