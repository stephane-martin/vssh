package main

import (
	"os"

	"github.com/google/gops/agent"
)

var gopsEnabled bool

func startAgent() {
	_, ok := os.LookupEnv("VSSH_GOPS")
	if !ok {
		return
	}
	err := agent.Listen(agent.Options{})
	if err == nil {
		gopsEnabled = true
	}
}

func stopAgent() {
	if gopsEnabled {
		agent.Close()
	}
}
