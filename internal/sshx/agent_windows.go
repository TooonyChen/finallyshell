package sshx

import (
	"io"
	"os"
	"strings"
)

// DefaultAgentAddr is the Windows OpenSSH agent service's named pipe,
// which Bitwarden's agent uses too.
func DefaultAgentAddr() string { return `\\.\pipe\openssh-ssh-agent` }

// openAgent opens the agent's named pipe, \\.\pipe\<name>, as a file,
// else its Unix socket, which Windows 10 has too.
func openAgent(addr string) (io.ReadWriteCloser, error) {
	if isPipe(addr) {
		return os.OpenFile(addr, os.O_RDWR, 0)
	}
	return dialUnix(addr)
}

// isPipe tells whether addr is a named pipe's path.
func isPipe(addr string) bool {
	a := strings.ReplaceAll(addr, "/", `\`)
	return strings.HasPrefix(a, `\\.\pipe\`) || strings.HasPrefix(a, `\\?\pipe\`)
}
