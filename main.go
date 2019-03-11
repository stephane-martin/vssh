package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/mitchellh/go-homedir"
	"github.com/stephane-martin/vault-exec/lib"
	"github.com/urfave/cli"
	"go.uber.org/zap"
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
			Name:   "password,W",
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
		cli.IntFlag{
			Name:   "sshport,rport,port,P",
			Usage:  "SSH remote port",
			EnvVar: "RPORT",
			Value:  22,
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
	}

	app.Action = VSSH

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

func VSSH(c *cli.Context) (e error) {
	defer func() {
		if e != nil {
			e = cli.NewExitError(e.Error(), 1)
		}
	}()
	loglevel := c.GlobalString("loglevel")
	logger, err := lib.Logger(loglevel)
	if err != nil {
		return err
	}
	defer logger.Sync()

	privkeyPath := c.GlobalString("privkey")
	if privkeyPath == "" {
		p, err := homedir.Expand("~/.ssh/id_rsa")
		if err != nil {
			return err
		}
		privkeyPath = p
	}
	infos, err := os.Stat(privkeyPath)
	if err != nil {
		return err
	}
	if !infos.Mode().IsRegular() {
		return errors.New("privkey is not a regular file")
	}

	privkeyb, err := ioutil.ReadFile(privkeyPath)
	if err != nil {
		return fmt.Errorf("failed to read private key file: %s", err)
	}
	if len(privkeyb) == 0 {
		return errors.New("empty private key")
	}
	pubkey, err := DerivePublicKey(privkeyb)
	if err != nil {
		return fmt.Errorf("error extracting public key: %s", err)
	}
	pubkeys := pubkey.Type() + " " + base64.StdEncoding.EncodeToString(pubkey.Marshal())

	sshMountPoint := c.GlobalString("mountpoint")
	if sshMountPoint == "" {
		return errors.New("empty SSH mount point")
	}

	rhost := c.GlobalString("sshhost")
	if rhost == "" {
		return errors.New("empty remote host")
	}

	role := c.GlobalString("role")
	if role == "" {
		return errors.New("empty SSH role")
	}

	authType := strings.ToLower(c.GlobalString("method"))
	path := strings.TrimSpace(c.GlobalString("path"))
	if path == "" {
		path = authType
	}
	os.Unsetenv("VAULT_ADDR")

	address := c.GlobalString("address")
	token := c.GlobalString("token")
	username := c.GlobalString("username")
	password := c.GlobalString("password")
	insecure := c.GlobalBool("insecure")
	port := c.GlobalInt("port")
	native := c.GlobalBool("native")

	client, err := lib.Auth(authType, address, path, token, username, password, logger)
	if err != nil {
		return fmt.Errorf("auth failed: %s", err)
	}
	err = lib.CheckHealth(client)
	if err != nil {
		return fmt.Errorf("vault health check error: %s", err)
	}
	sshuser := c.GlobalString("sshuser")
	if sshuser == "" {
		u, err := user.Current()
		if err != nil {
			return err
		}
		sshuser = u.Username
	}
	logger.Debugw(
		"vssh",
		"rhost", rhost,
		"ruser", sshuser,
		"privkey", privkeyPath,
		"role", role,
		"ssh_mount_point", sshMountPoint,
	)
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
		return fmt.Errorf("signing error: %s", err)
	}
	if signed, ok := sec.Data["signed_key"].(string); ok && len(signed) > 0 {
		logger.Debugw("signature success", "signed_key", strings.TrimSpace(signed))
		verbose := loglevel == "debug"
		err := Connect(rhost, sshuser, port, privkeyb, pubkeys, signed, c.Args(), verbose, insecure, native, logger)
		if err != nil {
			return err
		}
	} else {
		return errors.New("signature has failed")
	}
	return nil

}

func Connect(rhost, ruser string, port int, priv []byte, pub, signed string, args []string, verb, insecure, native bool, l *zap.SugaredLogger) error {
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
	// TODO: remove temp files as soon as possible
	defer os.Remove(pubkeyPath)
	err = ioutil.WriteFile(privkeyPath, append(priv, '\n'), 0600)
	if err != nil {
		return err
	}
	defer os.Remove(privkeyPath)
	err = ioutil.WriteFile(certPath, []byte(signed), 0600)
	if err != nil {
		return err
	}
	defer os.Remove(certPath)
	if native {
		l.Debugw("native SSH client")
		return Native(privkeyPath, ruser, rhost, port, args, verb, insecure)
	}
	l.Debugw("builtin SSH client")
	return GoSSH(privkeyPath, certPath, ruser, rhost, port, args, verb, insecure, l)
}
