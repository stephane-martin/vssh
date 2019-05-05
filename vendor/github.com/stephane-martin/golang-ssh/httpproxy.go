package ssh

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
)

type HTTPConnectProxy struct {
	HTTPS    bool
	Host     string
	HaveAuth bool
	Username string
	Password string
}

func NewHTTPConnectProxy(uri *url.URL) *HTTPConnectProxy {
	s := new(HTTPConnectProxy)
	s.HTTPS = uri.Scheme == "https"
	s.Host = uri.Host
	if uri.User != nil {
		s.HaveAuth = true
		s.Username = uri.User.Username()
		s.Password, _ = uri.User.Password()
	}
	return s
}

func (proxy *HTTPConnectProxy) DialContext(ctx context.Context, network, addr string) (c net.Conn, err error) {
	var d net.Dialer
	if proxy.HTTPS {
		var conn net.Conn
		conn, err = d.DialContext(ctx, "tcp", proxy.Host)
		if err != nil {
			return nil, err
		}
		tlsConn := tls.Client(conn, &tls.Config{
			ServerName: proxy.Host,
		})
		if ctx == nil {
			err = tlsConn.Handshake()
			if err != nil {
				_ = conn.Close()
				return nil, err
			}
		} else {
			errChan := make(chan error)
			go func() {
				errChan <- tlsConn.Handshake()
			}()
			select {
			case <-ctx.Done():
				_ = conn.Close()
				return nil, ctx.Err()
			case err = <-errChan:
				if err != nil {
					_ = conn.Close()
					return nil, err
				}
			}
		}
		c = tlsConn
	} else {
		c, err = d.DialContext(ctx, "tcp", proxy.Host)
	}
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = c.Close()
			c = nil
		}
	}()

	reqURL, err := url.Parse("http://" + addr)
	if err != nil {
		return c, err
	}
	reqURL.Scheme = ""

	req, err := http.NewRequest("CONNECT", reqURL.String(), nil)
	if err != nil {
		return c, err
	}
	req.Close = false
	if proxy.HaveAuth {
		req.SetBasicAuth(proxy.Username, proxy.Password)
	}
	req.Header.Set("User-Agent", "vssh")
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	err = req.Write(c)
	if err != nil {
		return c, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(c), req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return c, err
	}
	if resp.StatusCode != 200 {
		return c, fmt.Errorf("connect error: status %d", resp.StatusCode)
	}
	return c, nil
}
