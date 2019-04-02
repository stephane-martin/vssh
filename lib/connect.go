package lib

import (
	"context"
	"golang.org/x/crypto/ssh"

	"github.com/awnumar/memguard"
	"go.uber.org/zap"
)

func Connect(ctx context.Context, params SSHParams, priv, signed *memguard.LockedBuffer, pub *PublicKey, env map[string]string, l *zap.SugaredLogger) error {
	if params.Native {
		l.Debugw("native SSH client")
		return NativeConnect(ctx, params, priv, pub, signed, env, l)
	}
	l.Debugw("builtin SSH client")
	return GoConnect(ctx, params, priv, signed, env, l)
}

func ConnectAuth(ctx context.Context, params SSHParams, auth []ssh.AuthMethod, env map[string]string, l *zap.SugaredLogger) error {
	l.Debugw("builtin SSH client")
	return GoConnectAuth(ctx, params, auth, env, l)
}
