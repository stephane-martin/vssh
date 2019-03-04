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

func GetSecrets(ctx context.Context, client *api.Client, prefix bool, upcase bool, keys []string, logger *zap.SugaredLogger, results chan map[string]string) (rerr error) {
	g, lctx := errgroup.WithContext(ctx)
	defer func() {
		err := g.Wait()
		close(results)
		if err != nil {
			rerr = err
		}
	}()

	self, err := client.Auth().Token().RenewSelf(0)
	if err != nil {
		return fmt.Errorf("vault token lookup error: %s", err)
	}
	renewable, _ := self.TokenIsRenewable()
	if renewable {
		logger.Info("token is renewable")
		renewer, _ := client.NewRenewer(&api.RenewerInput{
			Secret: self,
		})
		g.Go(func() error {
			renewer.Renew()
			return nil
		})
		g.Go(func() error {
			<-lctx.Done()
			renewer.Stop()
			return lctx.Err()
		})
		g.Go(func() error {
			for {
				select {
				case err := <-renewer.DoneCh():
					return TokenNotRenewedError{Err: err}
				case renewal := <-renewer.RenewCh():
					logger.Infow("token renewed", "at", renewal.RenewedAt.Format(time.RFC3339), "lease", renewal.Secret.LeaseDuration)
				}
			}
		})
	} else {
		logger.Info("token is not renewable")
	}
	previousResult := make(map[string]string)
	for {
		subg, llctx := errgroup.WithContext(lctx)
		result, err := getSecretsHelper(llctx, subg, client, prefix, upcase, keys, logger)
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
			logger.Infow("can't renew secret", "key", e.Key, "at", time.Now().Format(time.RFC3339), "error", e.Err)
		} else {
			logger.Infow("secret has expired", "key", e.Key, "at", time.Now().Format(time.RFC3339))
		}
	}
}

func getSecretsHelper(ctx context.Context, g *errgroup.Group, client *api.Client, prefix bool, upcase bool, keys []string, logger *zap.SugaredLogger) (map[string]string, error) {
	fullResults := make(map[string]map[string]string)
	for _, s := range keys {
		sec := s
		res, err := client.Logical().Read(sec)
		if err != nil {
			return nil, fmt.Errorf("error reading secret from vault: %s", err)
		}
		logger.Debugw("secret read vault vault", "key", sec)
		fullResults[sec] = make(map[string]string)
		for k, v := range res.Data {
			if s, ok := v.(string); ok {
				fullResults[sec][k] = s
			} else if v != nil {
				fullResults[sec][k] = fmt.Sprintf("%s", v)
			}
		}
		if res.Renewable {
			logger.Infow("secret is renewable", "secret", sec)
			renewer, _ := client.NewRenewer(&api.RenewerInput{
				Secret: res,
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
				lease := res.LeaseDuration
				for {
					select {
					case err := <-renewer.DoneCh():
						lease = lease * 3 / 4
						if lease == 0 {
							return ExpiredSecretError{Err: err, Key: sec}
						}
						select {
						case <-ctx.Done():
							return ctx.Err()
						case <-time.After(time.Duration(lease) * time.Second):
							return ExpiredSecretError{Err: err, Key: sec}
						}
					case renewal := <-renewer.RenewCh():
						lease = renewal.Secret.LeaseDuration
						logger.Infow("secret renewed", "secret", sec, "at", renewal.RenewedAt.Format(time.RFC3339), "lease", lease)
					}
				}
			})
		} else {
			logger.Infow("secret is not renewable", "secret", sec)
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
			if upcase {
				k = strings.ToUpper(k)
			}
			result[k] = v
		}
	}
	return result, nil
}
