package sys

import (
	"os"

	"github.com/google/gops/agent"
)

var gopsEnabled bool

func StartAgent() {
	_, ok := os.LookupEnv("VSSH_GOPS")
	if !ok {
		return
	}
	err := agent.Listen(agent.Options{})
	if err == nil {
		gopsEnabled = true
	}
}

func StopAgent() {
	if gopsEnabled {
		agent.Close()
	}
}
