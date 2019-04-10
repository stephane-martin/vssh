package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/awnumar/memguard"
	"go.uber.org/zap"

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

func filterOutEmptyStrings(a []string) []string {
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

type entry struct {
	path  string
	rel   string
	isdir bool
}

func wrapPut(f putFunc) cli.ActionFunc {
	return func(c *cli.Context) (e error) {
		defer func() {
			if e != nil {
				e = cli.NewExitError(e.Error(), 1)
			}
		}()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cancelOnSignal(cancel)

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
		_, privkey, signed, _, err := getCredentials(ctx, c, sshParams.LoginName, logger)
		if err != nil {
			return err
		}

		sourcesNames := filterOutEmptyStrings(c.StringSlice("source"))
		if len(sourcesNames) == 0 {
			var paths []entry
			err := lib.WalkLocal(func(path, rel string, isdir bool) error {
				if strings.HasPrefix(rel, ".") {
					if isdir {
						return filepath.SkipDir
					}
					return nil
				}
				paths = append(paths, entry{path: path, rel: rel, isdir: isdir})
				return nil
			}, logger)
			if err != nil {
				return err
			}
			idx, _ := fuzzyfinder.FindMulti(paths, func(i int) string {
				if paths[i].isdir {
					return folderIcon + paths[i].rel
				}
				return fileIcon + paths[i].rel
			})
			for _, i := range idx {
				sourcesNames = append(sourcesNames, paths[i].path)
			}
		}
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

		return f(ctx, sources, dest, sshParams, privkey, signed, logger)
	}
}
