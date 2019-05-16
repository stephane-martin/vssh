package params

import "github.com/urfave/cli"

type CLIContext interface {
	VaultAddress() string
	VaultToken() string
	VaultAuthMethod() string
	VaultAuthPath() string
	VaultUsername() string
	VaultPassword() string
	VaultSSHMount() string
	VaultSSHRole() string
	SSHHost() string
	SSHCommand() []string
	SSHLogin() string
	SSHPort() int
	SSHPassword() bool
	SSHAgent() bool
	SSHInsecure() bool
	HTTPProxy() string
	PrivateKey() string
	VPrivateKey() string
	ForceTerminal() bool
}

func NewCliContext(ctx *cli.Context) CLIContext {
	return cliContext{ctx: ctx}
}

type cliContext struct {
	ctx *cli.Context
}

func (c cliContext) VaultAddress() string {
	return c.ctx.GlobalString("vault-address")
}

func (c cliContext) VaultToken() string {
	return c.ctx.GlobalString("vault-token")
}

func (c cliContext) VaultAuthMethod() string {
	return c.ctx.GlobalString("vault-auth-method")
}

func (c cliContext) VaultAuthPath() string {
	return c.ctx.GlobalString("vault-auth-path")
}

func (c cliContext) VaultUsername() string {
	return c.ctx.GlobalString("vault-username")
}

func (c cliContext) VaultPassword() string {
	return c.ctx.GlobalString("vault-password")
}

func (c cliContext) VaultSSHMount() string {
	return c.ctx.GlobalString("vault-ssh-mount")
}

func (c cliContext) VaultSSHRole() string {
	return c.ctx.GlobalString("vault-ssh-role")
}

func (c cliContext) SSHCommand() []string {
	if len(c.ctx.Args()) == 0 {
		return nil
	}
	return c.ctx.Args()[1:]
}

func (c cliContext) SSHHost() string {
	if len(c.ctx.Args()) == 0 {
		return ""
	}
	return c.ctx.Args()[0]
}

func (c cliContext) SSHLogin() string {
	return c.ctx.GlobalString("login")
}

func (c cliContext) SSHPort() int {
	return c.ctx.GlobalInt("ssh-port")
}

func (c cliContext) SSHPassword() bool {
	return c.ctx.GlobalBool("password")
}

func (c cliContext) SSHAgent() bool {
	return c.ctx.GlobalBool("agent")
}

func (c cliContext) SSHInsecure() bool {
	return c.ctx.GlobalBool("insecure")
}

func (c cliContext) HTTPProxy() string {
	return c.ctx.GlobalString("http-proxy")
}

func (c cliContext) PrivateKey() string {
	return c.ctx.GlobalString("privkey")
}

func (c cliContext) VPrivateKey() string {
	return c.ctx.GlobalString("vprivkey")
}

func (c cliContext) ForceTerminal() bool {
	return c.ctx.Bool("terminal")
}
