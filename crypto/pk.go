package crypto

import (
	"bytes"
	"crypto/dsa"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"runtime"
	"strings"

	"github.com/gdamore/tcell"
	"github.com/rivo/tview"

	"github.com/awnumar/memguard"
	"golang.org/x/crypto/ed25519"
	"golang.org/x/crypto/ssh"
)

// NeedPassphrase checks if the given private key needs a passphrase to be decoded.
func NeedPassphrase(privkey *memguard.LockedBuffer) (bool, error) {
	block, _ := pem.Decode(privkey.Buffer())
	if block == nil {
		return false, errors.New("failed to PEM-decode the private key")
	}
	memguard.WipeBytes(block.Bytes)
	return strings.Contains(block.Headers["Proc-Type"], "ENCRYPTED"), nil
}

// DecryptPrivateKey returns the decrypted version of the given private key, using a passphrase.
func DecryptPrivateKey(privkey, pass *memguard.LockedBuffer) (*memguard.LockedBuffer, error) {
	block, _ := pem.Decode(privkey.Buffer())
	if block == nil {
		return nil, errors.New("failed to PEM-decode the private key")
	}
	if x509.IsEncryptedPEMBlock(block) {
		typ := block.Type
		der, err := x509.DecryptPEMBlock(block, pass.Buffer())
		memguard.WipeBytes(block.Bytes)
		if err != nil {
			return nil, err
		}
		decryptedBlock := &pem.Block{Type: typ, Bytes: der}
		decryptedBuf := pem.EncodeToMemory(decryptedBlock)
		memguard.WipeBytes(der)
		memguard.WipeBytes(decryptedBlock.Bytes)
		decrypted, err := memguard.NewImmutableFromBytes(decryptedBuf)
		if err != nil {
			memguard.WipeBytes(decryptedBuf)
			return nil, err
		}
		return decrypted, nil
	}
	memguard.WipeBytes(block.Bytes)
	return privkey, nil
}

type PublicKey memguard.LockedBuffer

func (k *PublicKey) MarshalJSON() ([]byte, error) {
	buf, err := SerializePublicKey(k)
	if err != nil {
		return nil, err
	}
	res := make([]byte, 0, len(buf.Buffer())+2)
	res = append(res, '"')
	res = append(res, buf.Buffer()...)
	res = append(res, '"')
	return res, nil
}

func DerivePublicKey(privkey *memguard.LockedBuffer) (*PublicKey, error) {
	// newpublickey: *dsa.PrivateKey, *ecdsa.PublicKey, *dsa.PublicKey, ed25519.PublicKey
	// TODO: the private key content leaks in p. wipe it.
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
	pubBuf, err := memguard.NewImmutableFromBytes(pubBytes)
	if err != nil {
		memguard.WipeBytes(pubBytes)
		return nil, err
	}
	return (*PublicKey)(pubBuf), nil
}

func SerializePublicKey(public *PublicKey) (*memguard.LockedBuffer, error) {
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

func ReadCertificateFromFileSystem(path string) (*memguard.LockedBuffer, error) {
	certb, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate file: %s", err)
	}
	if len(certb) == 0 {
		return nil, errors.New("empty certificate file")
	}
	certb2 := append(bytes.Trim(certb, "\n"), '\n')
	cert, err := memguard.NewImmutableFromBytes(certb2)
	memguard.WipeBytes(certb)
	if err != nil {
		return nil, fmt.Errorf("failed to create memguard for certificate: %s", err)
	}
	return cert, nil
}

func ReadPrivateKeyFromFileSystem(path string) (*memguard.LockedBuffer, error) {
	privkeyb, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key file: %s", err)
	}
	if len(privkeyb) == 0 {
		return nil, errors.New("empty private key")
	}
	privkeyb2 := append(bytes.Trim(privkeyb, "\n"), '\n')
	privkey, err := memguard.NewImmutableFromBytes(privkeyb2)
	memguard.WipeBytes(privkeyb)
	if err != nil {
		return nil, fmt.Errorf("failed to create memguard for private key: %s", err)
	}
	needPass, err := NeedPassphrase(privkey)
	if err != nil {
		return nil, fmt.Errorf("error parsing private key: %s", err)
	}
	if needPass {
		pass, err := InputPassword("Enter the passphrase for the private key")
		if err != nil {
			return nil, fmt.Errorf("failed to get passphrase: %s", err)
		}
		decrypted, err := DecryptPrivateKey(privkey, pass)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt private key: %s", err)
		}
		privkey.Destroy()
		privkey = decrypted
	}
	return privkey, nil
}

func InputPassword(prompt string) (*memguard.LockedBuffer, error) {
	defer runtime.GC()
	app := tview.NewApplication()

	field := tview.NewInputField()
	field.SetLabel("Password:").SetText("").SetFieldWidth(0).SetMaskCharacter('*')
	field.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			field.SetText("")
		}
		app.Stop()
	})

	form := tview.NewForm()
	form.SetFieldBackgroundColor(tview.Styles.PrimitiveBackgroundColor)
	form.SetFieldTextColor(tcell.ColorRed)
	form.SetBorder(true).SetTitle(" " + prompt + " ").SetTitleAlign(tview.AlignLeft)
	form.AddFormItem(field)
	err := app.SetRoot(form, true).Run()
	if err != nil {
		return nil, err
	}
	pass := field.GetText()

	if len(pass) == 0 {
		return nil, errors.New("empty password")
	}
	b, err := memguard.NewImmutableFromBytes([]byte(pass))
	if err != nil {
		return nil, err
	}
	return b, nil
}
