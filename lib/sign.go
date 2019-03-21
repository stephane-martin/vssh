package lib

import (
	"errors"
	"runtime"

	"github.com/awnumar/memguard"
	"github.com/hashicorp/vault/api"
)

func Sign(pubkey *memguard.LockedBuffer, loginName string, vaultParams VaultParams, client *api.Client) (*memguard.LockedBuffer, error) {
	// the defer is here to try to erase the maximum private data from memory
	defer runtime.GC()
	serialized, err := SerializePublicKey(pubkey)
	if err != nil {
		return nil, err
	}
	sshc := client.SSH()
	sshc.MountPoint = vaultParams.SSHMount
	sec, err := sshc.SignKey(vaultParams.SSHRole, map[string]interface{}{
		"valid_principals": loginName,
		"public_key":       string(serialized.Buffer()),
		"cert_type":        "user",
	})
	serialized = nil
	if err != nil {
		return nil, err
	}
	signed, ok := sec.Data["signed_key"].(string)
	sec = nil
	if ok && len(signed) > 0 {
		res, err := memguard.NewImmutableFromBytes([]byte(signed))
		signed = ""
		if err != nil {
			return nil, err
		}
		return res, nil
	}
	return nil, errors.New("improper signed_key in Vault response")
}
