//go:build !windows

package sshx

import (
	"io"
	"os"
)

// DefaultAgentAddr is the agent's Unix socket of SSH_AUTH_SOCK, ""
// when it is not set.
func DefaultAgentAddr() string { return os.Getenv("SSH_AUTH_SOCK") }

// openAgent connects to the agent's Unix socket.
func openAgent(addr string) (io.ReadWriteCloser, error) { return dialUnix(addr) }
