package lib

import "net/url"

type VaultParams struct {
	Address    string
	Token      string
	AuthMethod string
	AuthPath   string
	Username   string
	Password   string
	SSHMount   string
	SSHRole    string
}

type SSHParams struct {
	Port      int
	Insecure  bool
	LoginName string
	Host      string
	Commands  []string
	HTTPProxy *url.URL
}

type Params struct {
	LogLevel string
	Upcase   bool
	Prefix   bool
}
