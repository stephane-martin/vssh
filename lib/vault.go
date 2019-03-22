package lib

import (
	"context"

	"github.com/hashicorp/vault/api"
	vexec "github.com/stephane-martin/vault-exec/lib"
	"go.uber.org/zap"
)

func GetSecretsFromVault(ctx context.Context, client *api.Client, keys []string, prefix, upcase bool, l *zap.SugaredLogger) (map[string]string, error) {
	c := make(chan map[string]string, 1)
	var err error
	lctx, lcancel := context.WithCancel(ctx)
	go func() {
		err = vexec.GetSecrets(
			lctx,
			client,
			prefix,
			upcase,
			true,
			keys,
			l,
			c,
		)
		close(c)
	}()
	secrets := <-c
	lcancel()
	return secrets, err
}
