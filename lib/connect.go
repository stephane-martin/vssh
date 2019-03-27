package lib

import (
	"context"

	"github.com/awnumar/memguard"
	"go.uber.org/zap"
)

func Connect(ctx context.Context, params SSHParams, priv, signed *memguard.LockedBuffer, pub *PublicKey, env map[string]string, l *zap.SugaredLogger) error {
	if params.Native {
		l.Debugw("native SSH client")
		return Native(ctx, params, priv, pub, signed, env, l)
	}
	l.Debugw("builtin SSH client")
	return GoSSH(ctx, params, priv, signed, env, l)
}
