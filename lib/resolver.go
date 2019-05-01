package lib

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
	cmap "github.com/orcaman/concurrent-map"
	"github.com/pkg/sftp"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

type Resolver struct {
	wrapped    *net.Resolver
	logger     *zap.SugaredLogger
	serverAddr string
	cache      cmap.ConcurrentMap
}

// TODO: cache results

var pending = "pending"

type ErrResolve struct {
	Err error
	t   time.Time
}

func (e ErrResolve) Error() string {
	return e.Err.Error()
}

func (e ErrResolve) Time() time.Time {
	return e.t
}

type CachedResult struct {
	IP net.IP
	t  time.Time
}

func (res CachedResult) Time() time.Time {
	return res.t
}

func NewResolver(client *ssh.Client, serverAddr string, logger *zap.SugaredLogger) *Resolver {
	r := new(Resolver)
	r.logger = logger
	r.serverAddr = serverAddr
	r.wrapped = &net.Resolver{
		PreferGo:     true,
		StrictErrors: false,
		Dial: func(_ context.Context, _ string, _ string) (net.Conn, error) {
			return client.Dial("tcp", serverAddr)
		},
	}
	r.cache = cmap.New()
	return r
}

func (r Resolver) lookup(ctx context.Context, name string) (net.IP, error) {
	addrs, err := r.wrapped.LookupIPAddr(ctx, name)
	if err != nil {
		return nil, err
	}
	if r.logger != nil {
		ips := make([]string, 0, len(addrs))
		for _, addr := range addrs {
			ips = append(ips, addr.String())
		}
		r.logger.Debugw("DNS result", "hostname", name, "server", r.serverAddr, "ips", strings.Join(ips, ","))
	}
	for _, addr := range addrs {
		ipv4 := addr.IP.To4()
		if ipv4 != nil {
			return ipv4, nil
		}
	}
	return addrs[0].IP, nil

}

func (r Resolver) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	if !r.cache.SetIfAbsent(name, pending) {
		for {
			now := time.Now()
			if val, ok := r.cache.Get(name); ok {
				if err, ok := val.(*ErrResolve); ok {
					if now.Sub(err.Time()) > (5 * time.Second) {
						// the cached err is too old
						// try to remove the cached err
						r.cache.RemoveCb(name, func(k string, v interface{}, exists bool) bool {
							return v == val
						})
						return r.Resolve(ctx, name)
					}
					if r.logger != nil {
						r.logger.Debugw("resolved to cached err", "hostname", name, "error", err.Err.Error())
					}
					return ctx, nil, err.Err
				}
				if res, ok := val.(*CachedResult); ok {
					if now.Sub(res.Time()) > (2 * time.Minute) {
						// the cached IP is too old
						// try to remove the cached result
						r.cache.RemoveCb(name, func(k string, v interface{}, exists bool) bool {
							return v == val
						})
						return r.Resolve(ctx, name)
					}
					if r.logger != nil {
						r.logger.Debugw("resolved to cached IP", "hostname", name, "ip", res.IP.String())
					}
					return ctx, res.IP, nil
				}
			}
			select {
			case <-ctx.Done():
				return ctx, nil, context.Canceled
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	ip, err := r.lookup(ctx, name)
	if err != nil {
		r.cache.Set(name, &ErrResolve{Err: err, t: time.Now()})
		if r.logger != nil {
			r.logger.Debugw("resolve error", "hostname", name, "error", err.Error())
		}
		return ctx, nil, err
	}
	r.cache.Set(name, &CachedResult{IP: ip, t: time.Now()})
	if r.logger != nil {
		r.logger.Debugw("resolved", "hostname", name, "ip", ip.String())
	}

	return ctx, ip, nil
}

func FindDNSServers(client *ssh.Client) ([]string, error) {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return nil, err
	}
	defer sftpClient.Close()
	f, err := sftpClient.Open("/etc/resolv.conf")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	config, err := dns.ClientConfigFromReader(f)
	if err != nil {
		return nil, err
	}
	return config.Servers, nil
}
