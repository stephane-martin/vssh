package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ahmetb/go-linq"
	"github.com/logrusorgru/aurora"
	"github.com/mattn/go-shellwords"
	"github.com/mitchellh/go-homedir"
	"github.com/peterh/liner"
	"github.com/stephane-martin/vssh/lib"
	"github.com/urfave/cli"
	"golang.org/x/crypto/ssh"
)

func sftpCommand() cli.Command {
	return cli.Command{
		Name:  "sftp",
		Usage: "download/upload files with sftp protocol using Vault for authentication",
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
		},
		Action: func(c *cli.Context) (e error) {
			defer func() {
				if e != nil {
					e = cli.NewExitError(e.Error(), 1)
				}
			}()

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

			// TODO: get rid of context.Background
			_, credentials, err := getCredentials(context.Background(), c, sshParams.LoginName, logger)
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

			client, err := lib.SFTPClient(sshParams, methods, logger)
			if err != nil {
				return err
			}
			defer func() { client.Close() }()

			state, err := newShellState(
				client,
				c.GlobalBool("pager"),
				func(info string) {
					fmt.Fprintln(os.Stderr, aurora.Blue("-> "+info))
				},
				func(err string) {
					fmt.Fprintln(os.Stderr, aurora.Red("===> "+err))
				},
			)
			if err != nil {
				return err
			}

			line := liner.NewLiner()
			defer line.Close()

			historyPath, err := homedir.Expand("~/.config/vssh/history")
			if err == nil {
				h, err := os.Open(historyPath)
				if err == nil {
					_, _ = line.ReadHistory(h)
				}
				_ = h.Close()
				defer func() {
					err := os.MkdirAll(filepath.Dir(historyPath), 0700)
					if err == nil {
						h, err := os.Create(historyPath)
						if err == nil {
							_, _ = line.WriteHistory(h)
							_ = h.Close()
						}
					}
				}()
			}

			commands := []string{
				"ls", "lls", "ll", "lll",
				"get", "put",
				"cd", "lcd",
				"edit", "ledit",
				"less", "lless",
				"mkdir", "lmkdir", "mkdirall", "lmkdirall",
				"pwd", "lpwd",
				"rename",
				"rm", "lrm", "rmdir", "lrmdir",
				"exit", "logout",
				"help",
			}
			line.SetCompleter(func(line string) []string {
				args, err := shellwords.Parse(line)
				if err != nil {
					return nil
				}
				if len(args) == 0 {
					return commands
				}
				cmdStart := strings.ToLower(args[0])
				if linq.From(commands).Contains(cmdStart) {
					props := state.complete(cmdStart, args[1:])
					if len(props) == 0 {
						return nil
					}
					linq.From(props).SelectT(func(p string) string { return cmdStart + " " + p }).ToSlice(&props)
					return props
				}
				if len(args) == 1 {
					var props []string
					linq.From(commands).WhereT(func(cmd string) bool { return strings.HasPrefix(cmd, cmdStart) }).ToSlice(&props)
					return props
				}
				return nil
			})
			line.SetCtrlCAborts(true)
			line.SetTabCompletionStyle(liner.TabCircular)

		L:
			for {
				termWidth := state.width() - 1
				shortLocalWD := shorten(state.LocalWD)
				promptWidth := 11 + len(state.RemoteWD) + len(shortLocalWD)
				moreSpaces := termWidth - promptWidth
				if moreSpaces <= 1 {
					moreSpaces = 1
				}
				spaces := strings.Repeat(" ", moreSpaces)
				fmt.Printf("┌─ R=[%s]%sL=[%s]\n", aurora.Cyan(state.RemoteWD), spaces, aurora.Cyan(shortLocalWD))
				l, err := line.Prompt("└╾ ")
				if err == liner.ErrPromptAborted {
					continue L
				}
				if err == io.EOF {
					return nil
				}
				if err == liner.ErrInvalidPrompt {
					return err
				}
				if err != nil {
					fmt.Fprintln(os.Stderr, aurora.Red(fmt.Sprintf("Failed to read line: %s", err)))
					continue L
				}
				line.AppendHistory(l)
				res, err := state.dispatch(l)
				if err == io.EOF {
					return nil
				}
				if err != nil {
					fmt.Fprintln(os.Stderr, aurora.Red("===> "+err.Error()))
					continue L
				}
				fmt.Print(res)
				if res != "" && !strings.HasSuffix(res, "\n") {
					fmt.Println()
				}
			}
		},
		Subcommands: []cli.Command{
			sftpPutCommand(),
			sftpGetCommand(),
			{
				Name: "less",
				Flags: []cli.Flag{
					cli.StringFlag{
						Name:  "target",
						Usage: "file to copy from the remote server",
					},
				},
				Usage: "show remote file",
				Action: func(c *cli.Context) (e error) {
					defer func() {
						if e != nil {
							e = cli.NewExitError(e.Error(), 1)
						}
					}()

					target := strings.TrimSpace(c.String("target"))
					if target == "" {
						return errors.New("target not specified")
					}

					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					sigchan := make(chan os.Signal, 1)
					signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)
					go func() {
						for range sigchan {
							cancel()
						}
					}()

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

					cb := func(isDir, endOfDir bool, name string, perms os.FileMode, mtime, atime time.Time, content io.Reader) error {
						if isDir {
							return errors.New("remote target is a directory")
						}
						b, err := ioutil.ReadAll(content)
						if err != nil {
							return err
						}
						return lib.ShowFile(name, b, c.GlobalBool("pager"))
					}
					return lib.SFTPGetAuth(ctx, []string{target}, sshParams, methods, cb, logger)
				},
			},
			{
				Name:  "list",
				Usage: "list remote files",
				Flags: []cli.Flag{
					cli.BoolFlag{
						Name:  "color",
						Usage: "colored output",
					},
					cli.BoolFlag{
						Name:  "hidden",
						Usage: "show hidden files and directories",
					},
				},
				Action: func(c *cli.Context) (e error) {
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

					hidden := c.Bool("hidden")
					aur := aurora.NewAurora(c.Bool("color"))
					return lib.SFTPListAuth(ctx, sshParams, methods, logger, func(path, relname string, isdir bool) error {
						if isdir {
							if strings.HasPrefix(filepath.Base(path), ".") {
								if hidden {
									fmt.Println(aur.Blue(relname + "/"))
								} else {
									return filepath.SkipDir
								}
							} else {
								fmt.Println(aur.Bold(aur.Blue(relname + "/")))
							}
						} else {
							if strings.HasPrefix(filepath.Base(path), ".") {
								if hidden {
									fmt.Println(aur.Gray(relname))
								}
							} else {
								fmt.Println(relname)
							}
						}
						return nil
					})
				},
			},
		},
	}
}
