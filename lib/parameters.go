package lib

import (
	"strings"

	"github.com/urfave/cli"
)

type VaultParams struct {
	Address    string
	Token      string
	AuthMethod string
	AuthPath   string
	Username   string
	Password   string
	SSHMount   string
	SSHRole    string
}

func GetVaultParams(c *cli.Context) VaultParams {
	p := VaultParams{
		SSHMount:   c.GlobalString("vault-ssh-mount"),
		SSHRole:    c.GlobalString("vault-ssh-role"),
		AuthMethod: strings.ToLower(c.GlobalString("vault-method")),
		AuthPath:   c.GlobalString("vault-auth-path"),
		Address:    c.GlobalString("vault-addr"),
		Token:      c.GlobalString("vault-token"),
		Username:   c.GlobalString("vault-username"),
		Password:   c.GlobalString("vault-password"),
	}
	if p.AuthMethod == "" {
		p.AuthMethod = "token"
	}
	if p.AuthPath == "" {
		p.AuthPath = p.AuthMethod
	}
	return p
}

type SSHParams struct {
	Port          int
	Insecure      bool
	Native        bool
	ForceTerminal bool
	Verbose       bool
	LoginName     string
	Host          string
	Commands      []string
}

type Params struct {
	LogLevel string
	Upcase   bool
	Prefix   bool
}
