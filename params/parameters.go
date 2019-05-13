package params

import (
	"errors"
	"net/url"
	"os/user"
	"strings"
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

type SSHParams struct {
	Port      int
	Insecure  bool
	LoginName string
	Host      string
	Commands  []string
	HTTPProxy *url.URL
}

type Params struct {
	LogLevel string
	Upcase   bool
	Prefix   bool
}

func GetSSHParams(c CLIContext) (p SSHParams, err error) {
	p.Host = strings.TrimSpace(c.SSHHost())
	if p.Host == "" {
		return p, errors.New("empty host")
	}
	spl := strings.SplitN(p.Host, "@", 2)
	if len(spl) == 2 {
		p.LoginName = spl[0]
		p.Host = spl[1]
	}
	if p.LoginName == "" {
		p.LoginName = c.SSHLogin()
		if p.LoginName == "" {
			u, err := user.Current()
			if err != nil {
				return p, err
			}
			p.LoginName = u.Username
		}
	}
	p.Commands = c.SSHCommand()
	p.Insecure = c.SSHInsecure()
	p.Port = c.SSHPort()
	if c.HTTPProxy() != "" {
		p.HTTPProxy, err = url.Parse(c.HTTPProxy())
	}
	return p, err
}
