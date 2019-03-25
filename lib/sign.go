package lib

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"runtime"

	"github.com/awnumar/memguard"
	"github.com/hashicorp/go-cleanhttp"
	"github.com/hashicorp/vault/api"
	"github.com/hashicorp/vault/helper/consts"
	"github.com/valyala/fastjson"
)

func Sign(ctx context.Context, pubkey *PublicKey, loginName string, vaultParams VaultParams, client *api.Client) (*memguard.LockedBuffer, error) {
	defer runtime.GC()
	data := map[string]interface{}{
		"valid_principals": loginName,
		"public_key":       pubkey,
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
	u, err := url.Parse(client.Address())
	if err != nil {
		return nil, err
	}
	u.Path = fmt.Sprintf("/v1/%s/sign/%s", vaultParams.SSHMount, vaultParams.SSHRole)
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
	r.Header.Set(consts.AuthHeaderName, client.Token())
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
	val = nil
	signed, err := memguard.NewImmutableFromBytes(s)
	if err != nil {
		memguard.WipeBytes(s)
		return nil, err
	}
	return signed, nil
}
