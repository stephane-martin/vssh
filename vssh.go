package main

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"os/user"
	"strings"
	"syscall"

	"github.com/awnumar/memguard"
	"github.com/mitchellh/go-homedir"
	vexec "github.com/stephane-martin/vault-exec/lib"
	"github.com/stephane-martin/vssh/lib"
	"github.com/urfave/cli"
)

func VSSH(c *cli.Context) (e error) {
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

	var params lib.Params
	var vaultParams lib.VaultParams
	var sshParams lib.SSHParams

	params.LogLevel = strings.ToLower(c.GlobalString("loglevel"))
	logger, err := vexec.Logger(params.LogLevel)
	if err != nil {
		return err
	}
	defer logger.Sync()

	params.Prefix = c.GlobalBool("prefix")
	params.Upcase = c.GlobalBool("upcase")
	sshParams.Verbose = params.LogLevel == "debug"

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
	privkey, err := memguard.NewImmutableFromBytes(privkeyb)
	if err != nil {
		return fmt.Errorf("failed to create memguard for private key: %s", err)
	}
	needPass, err := lib.NeedPassphrase(privkey)
	if err != nil {
		return fmt.Errorf("error parsing private key: %s", err)
	}
	if needPass {
		phrase, err := vexec.Input("enter the passphrase for the private key: ", true)
		if err != nil {
			return fmt.Errorf("failed to get passphrase: %s", err)
		}
		pass, err := memguard.NewImmutableFromBytes(phrase)
		if err != nil {
			return err
		}
		decrypted, err := lib.DecryptPrivateKey(privkey, pass)
		if err != nil {
			return fmt.Errorf("failed to decrypt private key: %s", err)
		}
		privkey.Destroy()
		privkey = decrypted
	}

	pubkey, err := lib.DerivePublicKey(privkey)
	if err != nil {
		return fmt.Errorf("error extracting public key: %s", err)
	}

	vaultParams.Secrets = c.GlobalStringSlice("secret")
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

	// unset env VAULT_ADDR to prevent the vault client from seeing it
	os.Unsetenv("VAULT_ADDR")

	vaultParams.Address = c.GlobalString("vault-addr")
	vaultParams.Token = c.GlobalString("vault-token")
	vaultParams.Username = c.GlobalString("vault-username")
	vaultParams.Password = c.GlobalString("vault-password")
	sshParams.Insecure = c.GlobalBool("insecure")
	sshParams.Port = c.GlobalInt("ssh-port")
	sshParams.Native = c.GlobalBool("native")
	sshParams.ForceTerminal = c.GlobalBool("t")

	client, err := vexec.Auth(
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
	err = vexec.CheckHealth(client)
	if err != nil {
		return fmt.Errorf("Vault health check error: %s", err)
	}
	var secrets map[string]string
	if len(vaultParams.Secrets) > 0 {
		c := make(chan map[string]string, 1)
		var err error
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			err = vexec.GetSecrets(
				ctx,
				client,
				params.Prefix,
				params.Upcase,
				true,
				vaultParams.Secrets,
				logger,
				c,
			)
			close(c)
		}()
		secrets = <-c
		cancel()
		if err != nil {
			return err
		}
	}

	signed, err := lib.Sign(pubkey, sshParams.LoginName, vaultParams, client)
	if err != nil {
		return fmt.Errorf("signing error: %s", err)
	}

	return lib.Connect(ctx, sshParams, privkey, pubkey, signed, secrets, logger)
}
