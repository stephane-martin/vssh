package crypto

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"runtime"

	"go.uber.org/zap"

	"github.com/awnumar/memguard"
	"github.com/hashicorp/go-cleanhttp"
	"github.com/hashicorp/vault/api"
	"github.com/valyala/fastjson"
)

func Sign(ctx context.Context, pub *PublicKey, login, sshMount, sshRole string, clt *api.Client, l *zap.SugaredLogger) (*memguard.LockedBuffer, error) {
	defer runtime.GC()
	data := map[string]interface{}{
		"valid_principals": login,
		"public_key":       pub,
		"cert_type":        "user",
	}
	buf, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	reqBody, err := memguard.NewImmutableFromBytes(buf)
	if err != nil {
		memguard.WipeBytes(buf)
		return nil, err
	}
	defer reqBody.Destroy()
	u, err := url.Parse(clt.Address())
	if err != nil {
		return nil, err
	}
	u.Path = fmt.Sprintf("/v1/%s/sign/%s", sshMount, sshRole)
	r := &http.Request{
		Method:        "PUT",
		URL:           u,
		Body:          ioutil.NopCloser(bytes.NewReader(reqBody.Buffer())),
		GetBody:       func() (io.ReadCloser, error) { return ioutil.NopCloser(bytes.NewReader(reqBody.Buffer())), nil },
		ContentLength: int64(len(reqBody.Buffer())),
		Close:         true,
		Host:          u.Host,
		Header:        make(http.Header),
	}
	r.Header.Set("X-Vault-Token", clt.Token())
	resp, err := cleanhttp.DefaultClient().Do(r.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	b, err := ioutil.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	respBody, err := memguard.NewImmutableFromBytes(b)
	if err != nil {
		memguard.WipeBytes(b)
		return nil, err
	}
	defer respBody.Destroy()
	p := new(fastjson.Parser)
	val, err := p.ParseBytes(respBody.Buffer())
	p = nil
	if err != nil {
		return nil, err
	}
	s := val.GetStringBytes("data", "signed_key")
	if len(s) == 0 {
		errStr := string(val.GetStringBytes("errors", "0"))
		if errStr != "" {
			return nil, errors.New(errStr)
		}
		l.Debugw("unexpected Vault response", "response", string(respBody.Buffer()))
		return nil, errors.New("unexpected Vault response")
	}
	val = nil
	signed, err := memguard.NewImmutableFromBytes(s)
	if err != nil {
		memguard.WipeBytes(s)
		return nil, err
	}
	return signed, nil
}
