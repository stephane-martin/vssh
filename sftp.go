package main

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

	"github.com/logrusorgru/aurora"
	"github.com/mattn/go-shellwords"
	"github.com/peterh/liner"
	vexec "github.com/stephane-martin/vault-exec/lib"
	"github.com/stephane-martin/vssh/lib"
	"github.com/urfave/cli"
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
		Action: func(c *cli.Context) error {
			commands := []string{"ls", "lls", "get", "put", "cd", "lcd", "lmkdir", "mkdir", "pwd", "lpwd", "rename", "rm", "rmdir", "exit", "help"}
			line := liner.NewLiner()
			defer line.Close()
			line.SetCompleter(func(line string) []string {
				args, err := shellwords.Parse(line)
				if err != nil {
					return nil
				}
				if len(args) == 0 {
					return commands
				}
				var c []string
				if len(args) == 1 {
					for _, n := range commands {
						if strings.HasPrefix(n, strings.ToLower(line)) {
							c = append(c, n)
						}
					}
				}
				return c
			})
			line.SetCtrlCAborts(true)
			line.SetTabCompletionStyle(liner.TabCircular)
		L:
			for {
				l, err := line.Prompt("> ")
				if err == nil {
					p := shellwords.NewParser()
					args, err := p.Parse(l)
					if err != nil {
						fmt.Fprintln(os.Stderr, "Error reading line: ", err)
					} else if p.Position != -1 {
						fmt.Fprintln(os.Stderr, "Error reading line")
					} else {
						for _, arg := range args {
							fmt.Println(arg)
						}
						switch args[0] {
						case "exit":
							break L
						}
					}
					//line.AppendHistory(name)
				} else if err == liner.ErrPromptAborted || err == io.EOF {
					break L
				} else {
					fmt.Fprintln(os.Stderr, "Error reading line: ", err)
				}
			}
			return nil
		},
		Subcommands: []cli.Command{
			sftpPutCommand(),
			sftpGetCommand(),
			{
				Name: "list",
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

					vaultParams := getVaultParams(c)
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

					_, privkey, signed, _, err := getCredentials(ctx, c, sshParams.LoginName, logger)
					if err != nil {
						return err
					}

					hidden := c.Bool("hidden")
					aur := aurora.NewAurora(c.Bool("color"))
					return lib.SFTPList(ctx, sshParams, privkey, signed, logger, func(path, relname string, isdir bool) error {
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
