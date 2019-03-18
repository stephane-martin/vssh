package lib

type VaultParams struct {
	Address    string
	Token      string
	AuthMethod string
	AuthPath   string
	Username   string
	Password   string
	SSHMount   string
	SSHRole    string
	Secrets    []string
}

type SSHParams struct {
	LoginName      string
	Host           string
	Port           int
	PrivateKeyPath string
	Insecure       bool
	Native         bool
	ForceTerminal  bool
	Commands       []string
	Verbose        bool
}

type Params struct {
	LogLevel string
	Upcase   bool
	Prefix   bool
}
