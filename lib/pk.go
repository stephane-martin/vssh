package lib

import (
	"crypto/dsa"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"strings"

	"github.com/awnumar/memguard"
	"golang.org/x/crypto/ed25519"
	"golang.org/x/crypto/ssh"
)

func NeedPassphrase(privkey *memguard.LockedBuffer) (bool, error) {
	block, _ := pem.Decode(privkey.Buffer())
	if block == nil {
		return false, errors.New("ssh: no key found")
	}
	return strings.Contains(block.Headers["Proc-Type"], "ENCRYPTED"), nil
}

func DecryptPrivateKey(privkey, pass *memguard.LockedBuffer) (*memguard.LockedBuffer, error) {
	block, _ := pem.Decode(privkey.Buffer())
	if x509.IsEncryptedPEMBlock(block) {
		der, err := x509.DecryptPEMBlock(block, pass.Buffer())
		if err != nil {
			return nil, err
		}
		decrypted, err := memguard.NewImmutableFromBytes(pem.EncodeToMemory(&pem.Block{Type: block.Type, Bytes: der}))
		if err != nil {
			return nil, err
		}
		return decrypted, nil
	}
	return privkey, nil
}

func DerivePublicKey(privkey *memguard.LockedBuffer) (pubBuf *memguard.LockedBuffer, err error) {
	// newpublickey: *dsa.PrivateKey, *ecdsa.PublicKey, *dsa.PublicKey, ed25519.PublicKey
	p, err := ssh.ParseRawPrivateKey(privkey.Buffer())
	if err != nil {
		return nil, err
	}
	var public ssh.PublicKey
	switch pk := p.(type) {
	case *dsa.PrivateKey:
		public, err = ssh.NewPublicKey(&pk.PublicKey)
	case *rsa.PrivateKey:
		public, err = ssh.NewPublicKey(&pk.PublicKey)
	case *ecdsa.PrivateKey:
		public, err = ssh.NewPublicKey(&pk.PublicKey)
	case *ed25519.PrivateKey:
		public, err = ssh.NewPublicKey(pk.Public().(ed25519.PublicKey))
	default:
		return nil, errors.New("unknown private key format")
	}
	if err != nil {
		return nil, err
	}
	pubBytes := public.Marshal()
	pubBuf, err = memguard.NewImmutableFromBytes(pubBytes)
	if err != nil {
		return nil, err
	}
	return pubBuf, nil
}

func SerializePublicKey(public *memguard.LockedBuffer) (*memguard.LockedBuffer, error) {
	pubkey, err := ssh.ParsePublicKey(public.Buffer())
	if err != nil {
		return nil, err
	}
	typ := []byte(pubkey.Type() + " ")
	typLBuf, err := memguard.NewImmutableFromBytes(typ)
	if err != nil {
		return nil, err
	}
	pubBuf := make([]byte, base64.StdEncoding.EncodedLen(len(public.Buffer())))
	base64.StdEncoding.Encode(pubBuf, public.Buffer())
	pubLBuf, err := memguard.NewImmutableFromBytes(pubBuf)
	if err != nil {
		return nil, err
	}
	res, err := memguard.Concatenate(typLBuf, pubLBuf)
	typLBuf.Destroy()
	pubLBuf.Destroy()
	if err != nil {
		return nil, err
	}
	return res, nil
}
