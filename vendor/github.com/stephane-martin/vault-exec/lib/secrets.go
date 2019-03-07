package lib

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/hashicorp/vault/api"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type TokenNotRenewedError struct {
	Err error
}

func (e TokenNotRenewedError) Error() string {
	if e.Err == nil {
		return "can't renew token"
	}
	return fmt.Sprintf("can't renew token: %s", e.Err)
}

type ExpiredSecretError struct {
	Err error
	Key string
}

func (e ExpiredSecretError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("secret expired %s", e.Key)
	}
	return fmt.Sprintf("can't renew secret %s: %s", e.Key, e.Err)
}

func GetSecrets(ctx context.Context, clt *api.Client, prefix, up, once bool, keys []string, l *zap.SugaredLogger, results chan map[string]string) (e error) {
	g, lctx := errgroup.WithContext(ctx)
	defer func() {
		err := g.Wait()
		if err != nil {
			e = err
		}
	}()

	self, err := clt.Auth().Token().RenewSelf(0)
	if err != nil {
		return fmt.Errorf("vault token lookup error: %s", err)
	}
	if !once {
		renewable, _ := self.TokenIsRenewable()
		if renewable {
			l.Info("token is renewable")
			renewToken(lctx, g, clt, self, l)
		} else {
			l.Info("token is not renewable")
		}
	}
	previousResult := make(map[string]string)
	for {
		subg, llctx := errgroup.WithContext(lctx)
		result, err := getSecretsHelper(llctx, subg, clt, prefix, up, once, keys, l)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(result, previousResult) {
			previousResult = result
			select {
			case results <- result:
			case <-lctx.Done():
				return lctx.Err()
			}
		}
		err = subg.Wait()
		if err == nil {
			// should not happen
			return context.Canceled
		}
		e, ok := err.(ExpiredSecretError)
		if !ok {
			return err
		}
		if e.Err != nil {
			l.Infow("can't renew secret", "key", e.Key, "at", time.Now().Format(time.RFC3339), "error", e.Err)
		} else {
			l.Infow("secret has expired", "key", e.Key, "at", time.Now().Format(time.RFC3339))
		}
	}
}

func getSecretsHelper(ctx context.Context, g *errgroup.Group, clt *api.Client, prefix bool, up, once bool, keys []string, l *zap.SugaredLogger) (map[string]string, error) {
	fullResults := make(map[string]map[string]string)
	for _, k := range keys {
		secretKey := k
		secret, err := clt.Logical().Read(secretKey)
		if err != nil {
			return nil, fmt.Errorf("error reading secret from vault: %s", err)
		}
		l.Debugw("secret read vault vault", "key", secretKey)
		fullResults[secretKey] = make(map[string]string)
		for k, v := range secret.Data {
			if s, ok := v.(string); ok {
				fullResults[secretKey][k] = s
			} else if v != nil {
				fullResults[secretKey][k] = fmt.Sprintf("%s", v)
			}
		}
		if once {
			g.Go(func() error {
				<-ctx.Done()
				return ctx.Err()
			})
		} else if secret.Renewable {
			l.Infow("secret is renewable", "secret", secretKey)
			renewSecret(ctx, g, secret, secretKey, clt, l)
		} else {
			l.Infow("secret is not renewable", "secret", secretKey)
			g.Go(func() error {
				<-ctx.Done()
				return ctx.Err()
			})
		}
	}

	result := make(map[string]string)

	for secretKey, sValues := range fullResults {
		for k, v := range sValues {
			if prefix {
				k = secretKey + "_" + k
			}
			k = sanitize(k)
			if up {
				k = strings.ToUpper(k)
			}
			result[k] = v
		}
	}
	return result, nil
}

func renewToken(ctx context.Context, g *errgroup.Group, clt *api.Client, token *api.Secret, l *zap.SugaredLogger) {
	renewer, _ := clt.NewRenewer(&api.RenewerInput{
		Secret: token,
	})
	g.Go(func() error {
		renewer.Renew()
		return nil
	})
	g.Go(func() error {
		<-ctx.Done()
		renewer.Stop()
		return ctx.Err()
	})
	g.Go(func() error {
		for {
			select {
			case err := <-renewer.DoneCh():
				return TokenNotRenewedError{Err: err}
			case renewal := <-renewer.RenewCh():
				l.Infow("token renewed", "at", renewal.RenewedAt.Format(time.RFC3339), "lease", renewal.Secret.LeaseDuration)
			}
		}
	})
}

func renewSecret(ctx context.Context, g *errgroup.Group, secret *api.Secret, secretKey string, clt *api.Client, l *zap.SugaredLogger) {
	renewer, _ := clt.NewRenewer(&api.RenewerInput{
		Secret: secret,
	})
	g.Go(func() error {
		renewer.Renew()
		return nil
	})
	g.Go(func() error {
		<-ctx.Done()
		renewer.Stop()
		return ctx.Err()
	})
	g.Go(func() error {
		lease := secret.LeaseDuration
		for {
			select {
			case err := <-renewer.DoneCh():
				lease = lease * 3 / 4
				if lease == 0 {
					return ExpiredSecretError{Err: err, Key: secretKey}
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(lease) * time.Second):
					return ExpiredSecretError{Err: err, Key: secretKey}
				}
			case renewal := <-renewer.RenewCh():
				lease = renewal.Secret.LeaseDuration
				l.Infow("secret renewed", "secret", secretKey, "at", renewal.RenewedAt.Format(time.RFC3339), "lease", lease)
			}
		}
	})

}
