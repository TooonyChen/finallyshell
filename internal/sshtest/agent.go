package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// Agent is an SSH agent for tests, in the test's process, on a Unix
// socket: it holds the keys AddKey gives it, and signs with them unless
// DenySigning was called, as Bitwarden does when the user denies a
// request.
type Agent struct {
	// Addr is its socket's path.
	Addr string

	keyring agent.ExtendedAgent
	ln      net.Listener
	open    atomic.Int64 // connections open
	conns   atomic.Int64 // connections made
	signs   atomic.Int64 // signatures asked for
	deny    atomic.Bool
	wg      sync.WaitGroup

	mu       sync.Mutex
	accepted []net.Conn // to close as the test ends
}

// StartAgent starts an agent with no keys, which stops as the test ends.
// It skips the test on Windows, whose agents are named pipes.
func StartAgent(t testing.TB) *Agent {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the test agent is a Unix socket")
	}
	// Not t.TempDir(): a socket's path is limited to about 100 bytes.
	dir, err := os.MkdirTemp("", "agent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	addr := filepath.Join(dir, "agent.sock")
	ln, err := net.Listen("unix", addr)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{Addr: addr, keyring: agent.NewKeyring().(agent.ExtendedAgent), ln: ln}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			a.open.Add(1)
			a.conns.Add(1)
			a.mu.Lock()
			a.accepted = append(a.accepted, conn)
			a.mu.Unlock()
			a.wg.Add(1)
			go func() {
				defer a.wg.Done()
				defer a.open.Add(-1)
				agent.ServeAgent(served{a.keyring, a}, conn)
				conn.Close()
			}()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		a.mu.Lock()
		for _, c := range a.accepted {
			c.Close()
		}
		a.mu.Unlock()
		a.wg.Wait()
	})
	return a
}

// AddKey adds a new Ed25519 key with the comment, and returns its public
// key, for a Server to AcceptKey.
func (a *Agent) AddKey(t testing.TB, comment string) ssh.PublicKey {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return a.add(t, key, pub, comment)
}

// AddRSAKey adds a new RSA key of bits with the comment, and returns its
// public key.
func (a *Agent) AddRSAKey(t testing.TB, bits int, comment string) ssh.PublicKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	return a.add(t, key, &key.PublicKey, comment)
}

func (a *Agent) add(t testing.TB, key, pub any, comment string) ssh.PublicKey {
	t.Helper()
	if err := a.keyring.Add(agent.AddedKey{PrivateKey: key, Comment: comment}); err != nil {
		t.Fatal(err)
	}
	p, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// DenySigning makes every request to sign fail from now on.
func (a *Agent) DenySigning() { a.deny.Store(true) }

// Open is how many connections to the agent are open.
func (a *Agent) Open() int { return int(a.open.Load()) }

// Conns is how many connections were made to the agent.
func (a *Agent) Conns() int { return int(a.conns.Load()) }

// Signs is how many signatures the agent was asked for.
func (a *Agent) Signs() int { return int(a.signs.Load()) }

// served is the keyring as the agent serves it: counting the signatures
// asked for, and refusing them after DenySigning.
type served struct {
	agent.ExtendedAgent
	a *Agent
}

var errDenied = errors.New("denied")

func (s served) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	return s.SignWithFlags(key, data, 0)
}

func (s served) SignWithFlags(key ssh.PublicKey, data []byte, flags agent.SignatureFlags) (*ssh.Signature, error) {
	s.a.signs.Add(1)
	if s.a.deny.Load() {
		return nil, errDenied
	}
	return s.ExtendedAgent.SignWithFlags(key, data, flags)
}
