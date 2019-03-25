package lib

import (
	"context"

	"github.com/awnumar/memguard"
	"go.uber.org/zap"
)

func Connect(ctx context.Context, sshParams SSHParams, priv *memguard.LockedBuffer, pub *PublicKey, signed *memguard.LockedBuffer, env map[string]string, l *zap.SugaredLogger) error {
	if sshParams.Native {
		l.Debugw("native SSH client")
		return Native(ctx, sshParams, priv, pub, signed, env, l)
	}
	l.Debugw("builtin SSH client")
	return GoSSH(ctx, sshParams, priv, signed, env, l)
}
