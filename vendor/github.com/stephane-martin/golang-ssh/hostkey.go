package ssh

import (
	"bytes"
	"fmt"
	"net"

	"github.com/mitchellh/go-homedir"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func MakeHostKeyCallback(insecure bool, l Logger) (ssh.HostKeyCallback, error) {
	if insecure {
		hkcb := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			l.Debugw(
				"host key",
				"hostname", hostname,
				"remote", remote.String(),
				"key", string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(key))),
			)
			return nil
		}
		return hkcb, nil
	}
	kh, err := homedir.Expand("~/.ssh/known_hosts")
	if err != nil {
		return nil, fmt.Errorf("failed to expand known_hosts path: %s", err)
	}
	callback, err := knownhosts.New(kh)
	if err != nil {
		return nil, fmt.Errorf("failed to open known_hosts file: %s", err)
	}
	hkcb := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		l.Debugw(
			"host key",
			"hostname", hostname,
			"remote", remote.String(),
			"key", string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(key))),
		)
		return callback(hostname, remote, key)
	}
	return hkcb, nil
}
