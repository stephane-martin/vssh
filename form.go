package main

import (
	"errors"
	"fmt"
	"os/user"
	"strconv"
	"strings"

	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
)

var authMethods = []string{"token", "userpass", "ldap", "approle"}

func t(s string) string {
	return strings.TrimSpace(s)
}

func idx(m string) int {
	for i, other := range authMethods {
		if m == other {
			return i
		}
	}
	return -1
}

func Form(c CLIContext) (CLIContext, error) {
	app := tview.NewApplication()

	form := tview.NewForm()
	form.SetButtonsAlign(tview.AlignCenter)
	form.SetBorder(true)
	form.SetTitle(" Enter connection parameters ")
	form.SetButtonBackgroundColor(tcell.ColorDarkBlue)
	form.SetButtonTextColor(tcell.ColorWhite)

	addInputField := func(label, value string, fieldWidth int, accept func(textToCheck string, lastChar rune) bool) *tview.InputField {
		field := tview.NewInputField().
			SetLabel(label).
			SetText(value).
			SetFieldWidth(fieldWidth).
			SetAcceptanceFunc(accept)
		form.AddFormItem(field)
		return field
	}

	addPasswordField := func(label string) *tview.InputField {
		field := tview.NewInputField().
			SetLabel(label).
			SetFieldWidth(32).
			SetMaskCharacter('*')
		form.AddFormItem(field)
		return field
	}

	addCheckBox := func(label string, checked bool) *tview.Checkbox {
		field := tview.NewCheckbox().
			SetLabel(label).
			SetChecked(checked)
		form.AddFormItem(field)
		return field
	}

	addDropDown := func(label string, choices []string, init string) *tview.DropDown {
		field := tview.NewDropDown().
			SetLabel(label).
			SetOptions(choices, nil)
		field.SetCurrentOption(idx(init))
		form.AddFormItem(field)
		return field
	}

	ctx := new(formContext)

	login := c.SSHLogin()
	if login == "" {
		u, err := user.Current()
		if err == nil {
			login = u.Username
		}
	}

	pkeyPath := c.PrivateKey()
	if pkeyPath == "" {
		pkeyPath = "~/.ssh/id_rsa"
	}

	ctx.sshHostField = addInputField("SSH host", c.SSHHost(), 40, nil)
	ctx.sshPortField = addInputField("SSH port", fmt.Sprintf("%d", c.SSHPort()), 5, tview.InputFieldInteger)
	ctx.sshLoginField = addInputField("SSH login", login, 40, nil)
	ctx.sshPKeyField = addInputField("SSH private key path", pkeyPath, 40, nil)
	ctx.sshVPKeyField = addInputField("SSH private key path in Vault", c.VPrivateKey(), 40, nil)
	ctx.sshPasswordField = addCheckBox("Use SSH password", c.SSHPassword())
	ctx.insecureField = addCheckBox("Do not check host key", c.SSHInsecure())
	ctx.vaultURLField = addInputField("Vault URL", c.VaultAddress(), 40, nil)
	ctx.vaultAuthMethodField = addDropDown("Vault authentication method", authMethods, c.VaultAuthMethod())
	ctx.vaultAuthPathField = addInputField("Vault authentication path", c.VaultAuthPath(), 40, nil)
	ctx.vaultTokenField = addInputField("Vault token", c.VaultToken(), 32, nil)
	ctx.vaultUsernameField = addInputField("Vault username", c.VaultUsername(), 40, nil)
	ctx.vaultPassField = addPasswordField("Vault password")
	ctx.vaultSSHMountField = addInputField("Vault SSH mount point", c.VaultSSHMount(), 40, nil)
	ctx.vaultSSHRoleField = addInputField("Vault SSH role", c.VaultSSHRole(), 40, nil)

	var confirm bool

	form.AddButton("Confirm ✓", func() {
		confirm = true
		app.Stop()
	})
	form.AddButton("Cancel 🗙", func() {
		app.Stop()
	})

	flex := tview.NewFlex()
	flex.AddItem(tview.NewBox(), 0, 1, false)
	flex.AddItem(form, 80, 0, true)
	flex.AddItem(tview.NewBox(), 0, 1, false)
	err := app.SetRoot(flex, true).Run()
	if err != nil {
		return nil, err
	}
	if !confirm {
		return nil, errors.New("canceled")
	}
	//fmt.Println(ctx.VaultAddress())
	return ctx, nil
}

type formContext struct {
	sshHostField         *tview.InputField
	sshPortField         *tview.InputField
	sshLoginField        *tview.InputField
	sshPasswordField     *tview.Checkbox
	sshPKeyField         *tview.InputField
	sshVPKeyField        *tview.InputField
	insecureField        *tview.Checkbox
	vaultURLField        *tview.InputField
	vaultAuthMethodField *tview.DropDown
	vaultAuthPathField   *tview.InputField
	vaultTokenField      *tview.InputField
	vaultUsernameField   *tview.InputField
	vaultPassField       *tview.InputField
	vaultSSHMountField   *tview.InputField
	vaultSSHRoleField    *tview.InputField
}

func (ctx *formContext) VaultAddress() string {
	return t(ctx.vaultURLField.GetText())
}

func (ctx *formContext) VaultToken() string {
	return t(ctx.vaultTokenField.GetText())
}

func (ctx *formContext) VaultAuthMethod() string {
	_, auth := ctx.vaultAuthMethodField.GetCurrentOption()
	if auth == "" {
		return "token"
	}
	return auth
}

func (ctx *formContext) VaultAuthPath() string {
	return t(ctx.vaultAuthPathField.GetText())
}

func (ctx *formContext) VaultUsername() string {
	return t(ctx.vaultUsernameField.GetText())
}

func (ctx *formContext) VaultPassword() string {
	return t(ctx.vaultPassField.GetText())
}

func (ctx *formContext) VaultSSHMount() string {
	return t(ctx.vaultSSHMountField.GetText())
}

func (ctx *formContext) VaultSSHRole() string {
	return t(ctx.vaultSSHRoleField.GetText())
}

func (ctx *formContext) SSHCommand() []string {
	return nil
}

func (ctx *formContext) SSHHost() string {
	return t(ctx.sshHostField.GetText())
}

func (ctx *formContext) SSHLogin() string {
	return t(ctx.sshLoginField.GetText())
}

func (ctx *formContext) SSHPort() int {
	port := t(ctx.sshPortField.GetText())
	p, _ := strconv.ParseInt(port, 10, 32)
	return int(p)
}

func (ctx *formContext) SSHPassword() bool {
	return ctx.sshPasswordField.IsChecked()
}

func (ctx *formContext) SSHInsecure() bool {
	return ctx.insecureField.IsChecked()
}

func (ctx *formContext) PrivateKey() string {
	return t(ctx.sshPKeyField.GetText())
}

func (ctx *formContext) VPrivateKey() string {
	return t(ctx.sshVPKeyField.GetText())
}
