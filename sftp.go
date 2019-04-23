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
		Action: func(clictx *cli.Context) (e error) {
			defer func() {
				if e != nil {
					e = cli.NewExitError(e.Error(), 1)
				}
			}()

			params := lib.Params{
				LogLevel: strings.ToLower(strings.TrimSpace(clictx.GlobalString("loglevel"))),
			}

			logger, err := Logger(params.LogLevel)
			if err != nil {
				return err
			}
			defer func() { _ = logger.Sync() }()

			var c CLIContext = cliContext{ctx: clictx}
			if c.SSHHost() == "" {
				var err error
				c, err = Form(c, false)
				if err != nil {
					return err
				}
			}
			sshParams, err := getSSHParams(c)
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
				clictx.GlobalBool("pager"),
				os.Stdout,
				func(f string, a ...interface{}) {
					fmt.Fprintln(os.Stderr, aurora.Blue("-> "+fmt.Sprintf(f, a...)))
				},
				func(f string, a ...interface{}) {
					fmt.Fprintln(os.Stderr, aurora.Red("===> "+fmt.Sprintf(f, a...)))
				},
			)
			if err != nil {
				return err
			}
			defer func() {
				_ = state.Close()
			}()

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
				"open", "lopen",
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
				var props []string
				lastSpace := line[len(line)-1] == ' '
				cmdStart := strings.ToLower(args[0])
				args = args[1:]
				if len(args) == 0 && !lastSpace {
					// try to complete the command
					linq.From(commands).WhereT(func(cmd string) bool { return strings.HasPrefix(cmd, cmdStart) }).ToSlice(&props)
					return props
				}
				if !linq.From(commands).Contains(cmdStart) {
					// unknown command
					return nil
				}
				if len(args) == 0 {
					// complete first empty argument
					props = state.Complete(cmdStart, nil, false)
				} else {
					// complete the last partial argument, or a last new empty argument
					props = state.Complete(cmdStart, args, lastSpace)
				}
				if len(props) == 0 {
					return nil
				}
				linq.From(props).SelectT(func(p string) string { return cmdStart + " " + p }).ToSlice(&props)
				return props
			})
			line.SetCtrlCAborts(true)
			line.SetTabCompletionStyle(liner.TabCircular)

		L:
			for {
				termWidth := state.Width() - 1
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
				err = state.Dispatch(l)
				if err == io.EOF {
					return nil
				}
				if err != nil {
					fmt.Fprintln(os.Stderr, aurora.Red("===> "+err.Error()))
					continue L
				}
				// fmt.Println()
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
				Action: func(clictx *cli.Context) (e error) {
					defer func() {
						if e != nil {
							e = cli.NewExitError(e.Error(), 1)
						}
					}()

					target := strings.TrimSpace(clictx.String("target"))
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
						LogLevel: strings.ToLower(strings.TrimSpace(clictx.GlobalString("loglevel"))),
					}

					logger, err := Logger(params.LogLevel)
					if err != nil {
						return err
					}
					defer func() { _ = logger.Sync() }()

					c := cliContext{ctx: clictx}
					if c.SSHHost() == "" {
						return errors.New("no host provided")
					}

					sshParams, err := getSSHParams(c)
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
						return lib.ShowFile(name, b, clictx.GlobalBool("pager"))
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
				Action: func(clictx *cli.Context) (e error) {
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
						LogLevel: strings.ToLower(strings.TrimSpace(clictx.GlobalString("loglevel"))),
					}

					logger, err := Logger(params.LogLevel)
					if err != nil {
						return err
					}
					defer func() { _ = logger.Sync() }()

					c := cliContext{ctx: clictx}
					if c.SSHHost() == "" {
						return errors.New("no host provided")
					}
					sshParams, err := getSSHParams(c)
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

					hidden := clictx.Bool("hidden")
					aur := aurora.NewAurora(clictx.Bool("color"))
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
