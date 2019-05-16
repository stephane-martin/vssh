package params

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

type Params struct {
	LogLevel string
	Upcase   bool
	Prefix   bool
}
