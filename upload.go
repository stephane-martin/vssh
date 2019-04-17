package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"

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
		Action: wrapPut(lib.ScpPutAuth),
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
		Action: wrapPut(lib.SFTPPutAuth),
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

type putFunc func(context.Context, []lib.Source, string, lib.SSHParams, []ssh.AuthMethod, *zap.SugaredLogger) error

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

		logger, err := Logger(params.LogLevel)
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

		_, credentials, err := getCredentials(ctx, c, sshParams.LoginName, logger)
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

		sourcesNames := filterOutEmptyStrings(c.StringSlice("source"))
		if len(sourcesNames) == 0 {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			sourcesNames, err = lib.FuzzyLocal(wd, logger)
			if err != nil {
				return err
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

		return f(ctx, sources, dest, sshParams, methods, logger)
	}
}
