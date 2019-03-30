package main

import (
	"fmt"
	"github.com/stephane-martin/vssh/lib"
	"github.com/urfave/cli"
	"os"
	"strings"
)

// App returns the vssh application object.
func App() *cli.App {
	app := cli.NewApp()
	app.Name = "vssh"
	app.Usage = "SSH/SCP using certificates signed by Vault"
	app.Version = Version
	app.Commands = []cli.Command{
		sshCommand(),
		uploadCommand(),
		downloadCommand(),
		{
			Name: "vis",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name: "flag",
					Value: "ALL",
				},
			},
			Action: func(c *cli.Context) error {
				flag := c.String("flag")
				iflag, ok := lib.C[strings.ToUpper(flag)]
				if !ok {
					return cli.NewExitError("unknown flag", 1)
				}
				args := c.Args()
				if len(args) == 0 {
					err := lib.StreamVis(os.Stdin, os.Stdout, iflag)
					if err != nil {
						return cli.NewExitError(err.Error(), 1)
					}
					return nil
				}
				fmt.Print(lib.StrVis(strings.Join(args, " "), iflag))
				return nil
			},
		},
		{
			Name: "unvis",
			Action: func(c *cli.Context) error {
				args := c.Args()
				if len(args) == 0 {
					return nil
				}
				s, err := lib.StrUnvis(strings.Join(args, " "))
				if err != nil {
					return cli.NewExitError(err.Error(), 1)
				}
				fmt.Print(s)
				return nil
			},
		},
	}
	app.Flags = GlobalFlags()
	return app
}

// GlobalFlags returns the global flags for vssh.
func GlobalFlags() []cli.Flag {
	return []cli.Flag{
		cli.StringFlag{
			Name:   "vault-address,vault-addr",
			Value:  "http://127.0.0.1:8200",
			EnvVar: "VAULT_ADDR",
			Usage:  "the address of the Vault server",
		},
		cli.StringFlag{
			Name:   "vault-token,token",
			Value:  "",
			EnvVar: "VAULT_TOKEN",
			Usage:  "Vault authentication token",
		},
		cli.StringFlag{
			Name:   "vault-auth-method,vault-method,method",
			Usage:  "type of authentication",
			Value:  "token",
			EnvVar: "VAULT_AUTH_METHOD",
		},
		cli.StringFlag{
			Name:   "vault-auth-path,vault-path,path",
			Usage:  "remote path in Vault where the chosen auth method is mounted",
			Value:  "",
			EnvVar: "VAULT_AUTH_PATH",
		},
		cli.StringFlag{
			Name:   "vault-username,U",
			Usage:  "Vault username or RoleID",
			Value:  "",
			EnvVar: "VAULT_USERNAME",
		},
		cli.StringFlag{
			Name:   "vault-password,P",
			Usage:  "Vault password or SecretID",
			Value:  "",
			EnvVar: "VAULT_PASSWORD",
		},
		cli.StringFlag{
			Name:   "vault-ssh-mount,mount,m",
			Usage:  "Vault SSH signer mount point",
			EnvVar: "VAULT_SSH_MOUNT",
			Value:  "ssh-client-signer",
		},
		cli.StringFlag{
			Name:   "vault-ssh-role,role",
			Usage:  "Vault signing role",
			EnvVar: "VAULT_SIGNING_ROLE",
		},
		cli.StringFlag{
			Name:  "loglevel",
			Usage: "logging level",
			Value: "info",
		},
	}
}
