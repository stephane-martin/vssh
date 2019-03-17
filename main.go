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

	var vaultParams VaultParams
	var sshParams SSHParams

	loglevel := strings.ToLower(c.GlobalString("loglevel"))
	logger, err := lib.Logger(loglevel)
	if err != nil {
		return err
	}
	defer logger.Sync()
	sshParams.Verbose = loglevel == "debug"

	args := c.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, c.App.UsageText)
		return errors.New("no host provided")
	}

	sshParams.Host = strings.TrimSpace(args[0])
	if sshParams.Host == "" {
		return errors.New("empty host")
	}
	spl := strings.SplitN(sshParams.Host, "@", 2)
	if len(spl) == 2 {
		sshParams.LoginName = spl[0]
		sshParams.Host = spl[1]
	}
	sshParams.Commands = args[1:]

	sshParams.PrivateKeyPath = c.GlobalString("privkey")
	if sshParams.PrivateKeyPath == "" {
		p, err := homedir.Expand("~/.ssh/id_rsa")
		if err != nil {
			return err
		}
		sshParams.PrivateKeyPath = p
	}
	infos, err := os.Stat(sshParams.PrivateKeyPath)
	if err != nil {
		return err
	}
	if !infos.Mode().IsRegular() {
		return errors.New("privkey is not a regular file")
	}

	privkeyb, err := ioutil.ReadFile(sshParams.PrivateKeyPath)
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
	pubkeyStr := pubkey.Type() + " " + base64.StdEncoding.EncodeToString(pubkey.Marshal())

	vaultParams.SSHMount = c.GlobalString("vault-sshmount")
	if vaultParams.SSHMount == "" {
		return errors.New("empty SSH mount point")
	}

	vaultParams.SSHRole = c.GlobalString("vault-sshrole")
	if vaultParams.SSHRole == "" {
		return errors.New("empty SSH role")
	}

	vaultParams.AuthMethod = strings.ToLower(c.GlobalString("vault-method"))
	if vaultParams.AuthMethod == "" {
		vaultParams.AuthMethod = "token"
	}
	vaultParams.AuthPath = strings.TrimSpace(c.GlobalString("vault-auth-path"))
	if vaultParams.AuthPath == "" {
		vaultParams.AuthPath = vaultParams.AuthMethod
	}
	os.Unsetenv("VAULT_ADDR")

	vaultParams.Address = c.GlobalString("vault-addr")
	vaultParams.Token = c.GlobalString("vault-token")
	vaultParams.Username = c.GlobalString("vault-username")
	vaultParams.Password = c.GlobalString("vault-password")
	sshParams.Insecure = c.GlobalBool("insecure")
	sshParams.Port = c.GlobalInt("ssh-port")
	sshParams.Native = c.GlobalBool("native")
	sshParams.ForceTerminal = c.GlobalBool("t")

	client, err := lib.Auth(
		vaultParams.AuthMethod,
		vaultParams.Address,
		vaultParams.AuthPath,
		vaultParams.Token,
		vaultParams.Username,
		vaultParams.Password,
		logger,
	)
	if err != nil {
		return fmt.Errorf("auth failed: %s", err)
	}
	err = lib.CheckHealth(client)
	if err != nil {
		return fmt.Errorf("vault health check error: %s", err)
	}

	if sshParams.LoginName == "" {
		sshParams.LoginName = c.GlobalString("login_name")
		if sshParams.LoginName == "" {
			u, err := user.Current()
			if err != nil {
				return err
			}
			sshParams.LoginName = u.Username
		}
	}

	logger.Debugw(
		"vssh",
		"ssh-host", sshParams.Host,
		"ssh-user", sshParams.LoginName,
		"privkey", sshParams.PrivateKeyPath,
		"vault-ssh-role", vaultParams.SSHRole,
		"vault-ssh-mount-point", vaultParams.SSHMount,
	)

	data := map[string]interface{}{
		"valid_principals": sshParams.LoginName,
		"public_key":       pubkeyStr,
		"cert_type":        "user",
	}

	logger.Debugw("public key to be signed", "pubkey", pubkeyStr)

	sshc := client.SSH()
	sshc.MountPoint = vaultParams.SSHMount
	sec, err := sshc.SignKey(vaultParams.SSHRole, data)
	if err != nil {
		return fmt.Errorf("signing error: %s", err)
	}
	if signed, ok := sec.Data["signed_key"].(string); ok && len(signed) > 0 {
		logger.Debugw("signature success", "signed_key", strings.TrimSpace(signed))
		return Connect(sshParams, privkeyb, pubkeyStr, signed, logger)
	}
	return errors.New("signature has failed")
}

func Connect(sshParams SSHParams, priv []byte, pub, signed string, l *zap.SugaredLogger) error {
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
	if sshParams.Native {
		l.Debugw("native SSH client")
		return Native(sshParams, privkeyPath, l)
	}
	l.Debugw("builtin SSH client")
	return GoSSH(sshParams, privkeyPath, certPath, l)
}
