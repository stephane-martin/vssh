package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/stephane-martin/vssh/lib"
	"github.com/urfave/cli"
)

// App returns the vssh application object.
func App() *cli.App {
	app := cli.NewApp()
	app.Name = "vssh"
	app.Usage = "SSH/SCP using certificates signed by Vault"
	app.Version = version
	app.Commands = []cli.Command{
		sshCommand(),
		scpCommand(),
		sftpCommand(),
		cli.Command{
			Name: "glob",
			Action: func(c *cli.Context) error {
				matches, err := filepath.Glob(c.Args()[0])
				if err != nil {
					return cli.NewExitError(err.Error(), 1)
				}
				for _, m := range matches {
					fmt.Println(m)
				}
				return nil
			},
		},

		cli.Command{
			Name:  "version",
			Usage: "print vssh version",
			Action: func(c *cli.Context) error {
				fmt.Println(version)
				return nil
			},
		},
		cli.Command{
			Name:  "less",
			Usage: "show local file content",
			Action: func(c *cli.Context) (e error) {
				defer func() {
					if e != nil {
						e = cli.NewExitError(e.Error(), 1)
					}
				}()
				args := c.Args()
				if len(args) != 1 {
					return errors.New("less takes one argument")
				}
				fname := args[0]
				content, err := ioutil.ReadFile(fname)
				if err != nil {
					return err
				}
				return lib.ShowFile(args[0], content, c.GlobalBool("pager"))
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
			Name:   "vault-username",
			Usage:  "Vault username or RoleID",
			Value:  "",
			EnvVar: "VAULT_USERNAME",
		},
		cli.StringFlag{
			Name:   "vault-password",
			Usage:  "Vault password or SecretID",
			Value:  "",
			EnvVar: "VAULT_PASSWORD",
		},
		cli.StringFlag{
			Name:   "vault-ssh-mount,mount",
			Usage:  "Vault SSH signer mount point",
			EnvVar: "VAULT_SSH_MOUNT",
			Value:  "ssh-client-signer",
		},
		cli.StringFlag{
			Name:   "vault-ssh-role,role",
			Usage:  "Vault signing role",
			EnvVar: "VAULT_SSH_ROLE",
		},

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
		cli.BoolFlag{
			Name:   "password",
			Usage:  "enable SSH password authentication",
			EnvVar: "VSSH_SSH_PASSWORD",
		},

		cli.StringFlag{
			Name:  "loglevel",
			Usage: "logging level",
			Value: "info",
		},
		cli.BoolFlag{
			Name:   "pager",
			Usage:  "use external pager",
			EnvVar: "VSSH_EXTERNAL_PAGER",
		},
	}
}
