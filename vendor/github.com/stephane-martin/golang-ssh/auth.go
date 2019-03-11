package ssh

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"golang.org/x/crypto/ssh"
)

// AuthPassword creates an AuthMethod for password authentication.
func AuthPassword(password string) ssh.AuthMethod {
	return ssh.Password(password)
}

// AuthKey creates an AuthMethod for SSH key authentication.
func AuthKey(r io.Reader) (ssh.AuthMethod, error) {
	key, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read key: %s", err)
	}
	privateKey, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %s", err)
	}
	return ssh.PublicKeys(privateKey), nil
}

// AuthKey creates an AuthMethod for SSH key authentication from a key file.
func AuthKeyFile(keyFilename string) (ssh.AuthMethod, error) {
	key, err := ioutil.ReadFile(keyFilename)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file %s: %s", keyFilename, err)
	}
	privateKey, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key %s: %s", keyFilename, err)
	}
	return ssh.PublicKeys(privateKey), nil
}

// AuthCert creates an AuthMethod for SSH certificate authentication from the key
// and certificate bytes.
func AuthCert(keyReader, certReader io.Reader) (ssh.AuthMethod, error) {
	key, err := ioutil.ReadAll(keyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %s", err)
	}
	cert, err := ioutil.ReadAll(certReader)
	if err != nil {
		return nil, fmt.Errorf("failed to reate certificate: %s", err)
	}
	privateKey, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %s", err)
	}
	certificate, err := ParseCertificate(cert)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %s", err)
	}
	signer, err := ssh.NewCertSigner(certificate, privateKey)
	if err != nil {
		return nil, fmt.Errorf("key and certificate do not match: %s", err)
	}
	return ssh.PublicKeys(signer), nil
}

// AuthCertFile creates an AuthMethod for SSH certificate authentication from the
// key and certicate files.
func AuthCertFile(keyFilename, certFilename string) (ssh.AuthMethod, error) {
	key, err := os.Open(keyFilename)
	if err != nil {
		return nil, fmt.Errorf("failed to open key file %s: %s", keyFilename, err)
	}
	defer key.Close()
	cert, err := os.Open(certFilename)
	if err != nil {
		return nil, fmt.Errorf("failed to open certificate file %s: %s", certFilename, err)
	}
	defer cert.Close()
	return AuthCert(key, cert)
}

func ParseCertificate(cert []byte) (*ssh.Certificate, error) {
	out, _, _, _, err := ssh.ParseAuthorizedKey(cert)
	if err != nil {
		return nil, err
	}
	c, ok := out.(*ssh.Certificate)
	if !ok {
		return nil, errors.New("the provided key is not a SSH certificate")
	}
	return c, nil
}
