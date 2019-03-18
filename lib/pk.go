package lib

import (
	"crypto/dsa"
	"crypto/ecdsa"
	"crypto/rsa"
	"errors"

	"golang.org/x/crypto/ed25519"
	"golang.org/x/crypto/ssh"
)

func DerivePublicKey(privkeyb []byte) (ssh.PublicKey, error) {
	// newpublickey: *dsa.PrivateKey, *ecdsa.PublicKey, *dsa.PublicKey, ed25519.PublicKey
	p, err := ssh.ParseRawPrivateKey(privkeyb)
	if err != nil {
		return nil, err
	}
	switch pk := p.(type) {
	case *dsa.PrivateKey:
		return ssh.NewPublicKey(&pk.PublicKey)
	case *rsa.PrivateKey:
		return ssh.NewPublicKey(&pk.PublicKey)
	case *ecdsa.PrivateKey:
		return ssh.NewPublicKey(&pk.PublicKey)
	case *ed25519.PrivateKey:
		return ssh.NewPublicKey(pk.Public().(ed25519.PublicKey))
	default:
		return nil, errors.New("unknown private key format")
	}
}
