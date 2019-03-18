package main

import (
	"github.com/urfave/cli"
)

func App() *cli.App {
	app := cli.NewApp()
	app.Name = "vssh"
	app.Usage = "SSH to remote server using certificate signed by vault"
	app.UsageText = "vault-ssh [options] host [cmd-to-execute]"
	app.Version = Version
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   "vault-address,vault-addr",
			Value:  "http://127.0.0.1:8200",
			EnvVar: "VAULT_ADDR",
			Usage:  "The address of the Vault server",
		},
		cli.StringFlag{
			Name:   "vault-token,token",
			Value:  "",
			EnvVar: "VAULT_TOKEN",
			Usage:  "Vault authentication token",
		},
		cli.StringFlag{
			Name:   "vault-method,method",
			Usage:  "type of authentication",
			Value:  "token",
			EnvVar: "VAULT_AUTH_METHOD",
		},
		cli.StringFlag{
			Name:   "vault-auth-path,path",
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
			Name:   "vault-sshmount,mount,m",
			Usage:  "Vault SSH signer mount point",
			EnvVar: "VSSH_SSH_MOUNT",
			Value:  "ssh-client-signer",
		},
		cli.StringFlag{
			Name:   "vault-sshrole,role",
			Usage:  "Vault signing role",
			EnvVar: "VSSH_SIGNING_ROLE",
		},
		cli.StringFlag{
			Name:  "loglevel",
			Usage: "logging level",
			Value: "info",
		},
		cli.StringFlag{
			Name:   "login_name,ssh-user,l",
			Usage:  "SSH remote user",
			EnvVar: "SSH_USER",
		},
		cli.IntFlag{
			Name:   "ssh-port,p",
			Usage:  "SSH remote port",
			EnvVar: "SSH_PORT",
			Value:  22,
		},
		cli.StringFlag{
			Name:   "privkey,private,identity,i",
			Usage:  "path to the SSH public key to be signed",
			EnvVar: "IDENTITY",
			Value:  "",
		},
		cli.BoolFlag{
			Name:   "insecure",
			Usage:  "do not check the remote SSH host key",
			EnvVar: "VSSH_INSECURE",
		},
		cli.BoolFlag{
			Name:   "native",
			Usage:  "use the native SSH client instead of the builtin one",
			EnvVar: "VSSH_NATIVE",
		},
		cli.BoolFlag{
			Name:   "t",
			Usage:  "force pseudo-terminal allocation",
			EnvVar: "VSSH_FORCE_PSEUDO",
		},
		cli.StringSliceFlag{
			Name:  "secret,key",
			Usage: "path of a secret to be read from Vault (multiple times)",
		},
		cli.BoolFlag{
			Name:   "upcase,up",
			Usage:  "convert all environment variable keys to uppercase",
			EnvVar: "UPCASE",
		},
		cli.BoolFlag{
			Name:   "prefix",
			Usage:  "prefix the environment variable keys with names of secrets",
			EnvVar: "PREFIX",
		},
	}
	return app
}
