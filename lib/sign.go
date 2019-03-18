package lib

import (
	"errors"

	"github.com/hashicorp/vault/api"
)

func Sign(pubkey, loginName string, vaultParams VaultParams, client *api.Client) (string, error) {
	data := map[string]interface{}{
		"valid_principals": loginName,
		"public_key":       pubkey,
		"cert_type":        "user",
	}
	sshc := client.SSH()
	sshc.MountPoint = vaultParams.SSHMount
	sec, err := sshc.SignKey(vaultParams.SSHRole, data)
	if err != nil {
		return "", err
	}
	if signed, ok := sec.Data["signed_key"].(string); ok && len(signed) > 0 {
		return signed, nil
	}
	return "", errors.New("improper signed_key in Vault response")
}
