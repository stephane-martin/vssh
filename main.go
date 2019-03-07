package main

import (
	"crypto/dsa"
	"crypto/ecdsa"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/mitchellh/go-homedir"
	"github.com/stephane-martin/vault-exec/lib"
	"github.com/urfave/cli"
	"go.uber.org/zap"
	"golang.org/x/crypto/ed25519"
	"golang.org/x/crypto/ssh"
)

var Version string

func main() {
	app := cli.NewApp()
	app.Name = "vssh"
	app.Usage = "SSH to remote server using certificate signed by vault"
	app.UsageText = "vault-ssh [options] [cmd-to-execute]"
	app.Version = Version
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   "address,vault-addr,addr",
			Value:  "http://127.0.0.1:8200",
			EnvVar: "VAULT_ADDR",
			Usage:  "The address of the Vault server",
		},
		cli.StringFlag{
			Name:   "token,t",
			Value:  "",
			EnvVar: "VAULT_TOKEN",
			Usage:  "Vault authentication token",
		},
		cli.StringFlag{
			Name:   "method,meth",
			Usage:  "type of authentication",
			Value:  "token",
			EnvVar: "VAULT_AUTH_METHOD",
		},
		cli.StringFlag{
			Name:   "path",
			Usage:  "remote path in Vault where the chosen auth method is mounted",
			Value:  "",
			EnvVar: "VAULT_AUTH_PATH",
		},
		cli.StringFlag{
			Name:   "username,U",
			Usage:  "Vault username or RoleID",
			Value:  "",
			EnvVar: "VAULT_USERNAME",
		},
		cli.StringFlag{
			Name:   "password,P",
			Usage:  "Vault password or SecretID",
			Value:  "",
			EnvVar: "VAULT_PASSWORD",
		},
		cli.StringFlag{
			Name:  "loglevel",
			Usage: "logging level",
			Value: "info",
		},
		cli.StringFlag{
			Name:   "sshuser,u",
			Usage:  "SSH remote user",
			EnvVar: "USER",
		},
		cli.StringFlag{
			Name:   "sshhost,rhost,host,H",
			Usage:  "SSH remote host",
			EnvVar: "RHOST",
		},
		cli.StringFlag{
			Name:   "privkey,private,identity,i",
			Usage:  "path to the SSH public key to be signed",
			EnvVar: "IDENTITY",
			Value:  "",
		},
		cli.StringFlag{
			Name:   "mountpoint,m",
			Usage:  "vault SSH mount point",
			EnvVar: "SIGNER",
			Value:  "ssh-client-signer",
		},
		cli.StringFlag{
			Name:   "role,r",
			Usage:  "Vault signing role",
			EnvVar: "ROLE",
		},
	}
	app.Action = func(c *cli.Context) error {
		loglevel := c.GlobalString("loglevel")
		logger, err := lib.Logger(loglevel)
		if err != nil {
			return cli.NewExitError(err.Error(), 1)
		}
		defer logger.Sync()

		privkeyPath := c.GlobalString("privkey")
		if privkeyPath == "" {
			p, err := homedir.Expand("~/.ssh/id_rsa")
			if err != nil {
				return cli.NewExitError(err.Error(), 1)
			}
			privkeyPath = p
		}
		infos, err := os.Stat(privkeyPath)
		if err != nil {
			return cli.NewExitError(err.Error(), 1)
		}
		if !infos.Mode().IsRegular() {
			return cli.NewExitError("privkey is not a regular file", 1)
		}

		privkeyb, err := ioutil.ReadFile(privkeyPath)
		if err != nil {
			return cli.NewExitError(fmt.Sprintf("failed to read private key file: %s", err), 1)
		}
		if len(privkeyb) == 0 {
			return cli.NewExitError("empty private key", 1)
		}
		pubkey, err := DerivePublicKey(privkeyb)
		if err != nil {
			return cli.NewExitError(fmt.Sprintf("error extracting public key: %s", err), 1)
		}
		pubkeys := pubkey.Type() + " " + base64.StdEncoding.EncodeToString(pubkey.Marshal())

		sshMountPoint := c.GlobalString("mountpoint")
		if sshMountPoint == "" {
			return cli.NewExitError("empty SSH mount point", 1)
		}

		rhost := c.GlobalString("sshhost")
		if rhost == "" {
			return cli.NewExitError("empty remote host", 1)
		}

		role := c.GlobalString("role")
		if role == "" {
			return cli.NewExitError("empty SSH role", 1)
		}

		authType := strings.ToLower(c.GlobalString("method"))
		path := strings.TrimSpace(c.GlobalString("path"))
		if path == "" {
			path = authType
		}
		os.Unsetenv("VAULT_ADDR")

		client, err := lib.Auth(authType, c.GlobalString("address"), path, c.GlobalString("token"), c.GlobalString("username"), c.GlobalString("password"), logger)
		if err != nil {
			return cli.NewExitError(fmt.Sprintf("auth failed: %s", err), 1)
		}

		err = lib.CheckHealth(client)
		if err != nil {
			return cli.NewExitError(fmt.Sprintf("vault health check error: %s", err), 1)
		}
		sshuser := c.GlobalString("sshuser")
		if sshuser == "" {
			u, err := user.Current()
			if err != nil {
				return cli.NewExitError(err.Error(), 1)
			}
			sshuser = u.Username
		}
		logger.Debugw("vssh", "rhost", rhost, "ruser", sshuser, "privkey", privkeyPath, "role", role, "ssh_mount_point", sshMountPoint)
		data := map[string]interface{}{
			"valid_principals": sshuser,
			"public_key":       pubkeys,
			"cert_type":        "user",
		}
		logger.Debugw("public key to be signed", "pubkey", pubkeys)
		sshc := client.SSH()
		sshc.MountPoint = sshMountPoint
		sec, err := sshc.SignKey(role, data)
		if err != nil {
			return cli.NewExitError(fmt.Sprintf("signing error: %s", err), 1)
		}
		if signed, ok := sec.Data["signed_key"].(string); ok && len(signed) > 0 {
			logger.Debugw("signature success", "signed_key", signed)
			err := Connect(rhost, sshuser, privkeyb, pubkeys, signed, c.Args(), loglevel == "debug", logger)
			if err != nil {
				return cli.NewExitError(err.Error(), 1)
			}
		} else {
			return cli.NewExitError("signature has failed", 1)
		}
		return nil
	}

	cli.OsExiter = func(code int) {
		os.Stdout.Sync()
		os.Stderr.Sync()
		if code != 0 {
			time.Sleep(200 * time.Millisecond)
			os.Exit(code)
		}
	}
	_ = app.Run(os.Args)
	cli.OsExiter(0)
}

func Connect(rhost string, ruser string, priv []byte, pub string, signed string, args []string, verb bool, l *zap.SugaredLogger) error {
	dir, err := ioutil.TempDir("", "vssh")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %s", err)
	}
	l.Debugw("using temp directory", "dirname", dir)
	defer os.RemoveAll(dir)
	pubkeyPath := filepath.Join(dir, "key.pub")
	privkeyPath := filepath.Join(dir, "key")
	certPath := filepath.Join(dir, "key-cert.pub")
	err = ioutil.WriteFile(pubkeyPath, append([]byte(pub), '\n'), 0600)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(privkeyPath, append(priv, '\n'), 0600)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(certPath, []byte(signed), 0600)
	if err != nil {
		return err
	}
	var allArgs []string
	if verb {
		allArgs = append(allArgs, "-v")
	}
	allArgs = append(allArgs, "-i", privkeyPath, "-l", ruser, rhost)
	allArgs = append(allArgs, args...)
	cmd := exec.Command("ssh", allArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func DerivePublicKey(privkeyb []byte) (ssh.PublicKey, error) {
	// newpublickey: *dsa.PrivateKey, *ecdsa.PublicKey, *dsa.PublicKey, ed25519.PublicKey
	p, err := ssh.ParseRawPrivateKey(privkeyb)
	if err != nil {
		return nil, err
	}
	switch pk := p.(type) {
	case *dsa.PrivateKey:
		return ssh.NewPublicKey(&pk.PublicKey)
	case *rsa.PrivateKey:
		return ssh.NewPublicKey(&pk.PublicKey)
	case *ecdsa.PrivateKey:
		return ssh.NewPublicKey(&pk.PublicKey)
	case *ed25519.PrivateKey:
		return ssh.NewPublicKey(pk.Public().(ed25519.PublicKey))
	default:
		return nil, errors.New("unknown private key format")
	}
}
