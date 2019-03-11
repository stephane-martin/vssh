Origin README see [here](https://github.com/glinton/ssh/blob/master/README.md)

This ssh package contains helpers for working with ssh in go.  The `client.go` file
is a modified version of `docker/machine/libmachine/ssh/client.go` that only
uses golang's native ssh client. It has also been improved to resize the tty as
needed. The key functions are meant to be used by either client or server
and will generate/store keys if not found.

## Usage:

```go
package main

import (
	"fmt"

	"github.com/nanobox-io/golang-ssh"
)

func main() {
	err := connect()
	if err != nil {
		fmt.Printf("Failed to connect - %s\n", err)
	}
}

func connect() error {
    client, err := ssh.NewNativeClient("user", "localhost", "SSH-2.0-MyCustomClient-1.0", 2222, nil, ssh.AuthPassword("pass"))
	if err != nil {
		return fmt.Errorf("Failed to create new client - %s", err)
	}

	err = client.Shell()
	if err != nil && err.Error() != "exit status 255" {
		return fmt.Errorf("Failed to request shell - %s", err)
	}

	return nil
}
```

## Compile for Windows:

If you get this error:

> go: github.com/Sirupsen/logrus@v1.2.0: parsing go.mod: unexpected module path "github.com/sirupsen/logrus"

when compile for Windows with `go mod`, see [this issue](https://github.com/golang/go/issues/26208)
for some hints and there was a walkaround:

    go mod init
    go get github.com/docker/docker@v0.0.0-20180422163414-57142e89befe
    GOOS=windows GOARCH=amd64 go build

