package main

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
