package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/stephane-martin/vssh/crypto"
	"github.com/stephane-martin/vssh/params"

	"github.com/ktr0731/go-fuzzyfinder"
	"golang.org/x/crypto/ssh"

	icon "github.com/stephane-martin/vssh/c"
	"github.com/stephane-martin/vssh/lib"
	"github.com/urfave/cli"
	"go.uber.org/zap"
)

func SCPGetCommand() cli.Command {
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
		Action: wrapGet(false),
	}
}

func SFTPGetCommand() cli.Command {
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
		Action: wrapGet(true),
	}
}

type getFunc func(context.Context, []string, params.SSHParams, []ssh.AuthMethod, lib.Callback, *zap.SugaredLogger) error

func wrapGet(sftp bool) cli.ActionFunc {
	return func(clictx *cli.Context) (e error) {
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

		sources := filterOutEmptyStrings(clictx.StringSlice("target"))
		if len(sources) == 0 && !sftp {
			return errors.New("you must specify the targets")
		}

		dest := strings.TrimSpace(clictx.String("destination"))
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

		gparams := params.Params{
			LogLevel: strings.ToLower(strings.TrimSpace(clictx.GlobalString("loglevel"))),
		}

		logger, err := params.Logger(gparams.LogLevel)
		if err != nil {
			return err
		}
		defer func() { _ = logger.Sync() }()

		if len(clictx.Args()) == 0 {
			return errors.New("no host provided")
		}
		c := params.NewCliContext(clictx)
		sshParams, err := params.GetSSHParams(c)
		if err != nil {
			return err
		}

		if len(sources) == 0 {
			var paths []entry

			_, credentials, err := crypto.GetSSHCredentials(ctx, c, sshParams.LoginName, logger)
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

			err = lib.SFTPListAuth(ctx, sshParams, methods, logger, func(path, rel string, isdir bool) error {
				if strings.HasPrefix(rel, ".") {
					if isdir {
						return filepath.SkipDir
					}
					return nil
				}
				paths = append(paths, entry{path: path, rel: rel, isdir: isdir})
				return nil
			})

			if err != nil {
				return err
			}
			idx, _ := fuzzyfinder.FindMulti(paths, func(i int) string {
				if paths[i].isdir {
					return icon.FolderIcon + paths[i].rel
				}
				return icon.FileIcon + paths[i].rel
			})
			for _, i := range idx {
				sources = append(sources, paths[i].path)
			}
			if len(sources) == 0 {
				return errors.New("you must specify the targets")
			}
		}

		_, credentials, err := crypto.GetSSHCredentials(ctx, c, sshParams.LoginName, logger)
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

		var f getFunc
		if sftp {
			f = lib.SFTPGetAuth
		} else {
			f = lib.ScpGetAuth
		}

		return f(
			ctx,
			sources,
			sshParams,
			methods,
			makeCB(
				dest,
				clictx.Bool("preserve"),
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
