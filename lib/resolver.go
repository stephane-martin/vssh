package lib

import (
	"context"
	"net"
	"strings"

	"github.com/miekg/dns"
	"github.com/pkg/sftp"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

type Resolver struct {
	wrapped    *net.Resolver
	logger     *zap.SugaredLogger
	serverAddr string
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
	return r
}

func (r Resolver) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	addrs, err := r.wrapped.LookupIPAddr(ctx, name)
	if err != nil {
		return ctx, nil, err
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
			return ctx, ipv4, nil
		}
	}
	return ctx, addrs[0].IP, nil
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
