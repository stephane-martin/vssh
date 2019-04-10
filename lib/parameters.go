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
}

type SSHParams struct {
	Port          int
	Insecure      bool
	Native        bool
	ForceTerminal bool
	Verbose       bool
	LoginName     string
	Host          string
	Commands      []string
}

type Params struct {
	LogLevel string
	Upcase   bool
	Prefix   bool
}
