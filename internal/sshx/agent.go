package sshx

// SSH agents: logging in with the keys an agent holds (config.AuthAgent),
// such as OpenSSH's ssh-agent, Bitwarden's, 1Password's or the Windows
// OpenSSH agent service, and listing them for the app's Check buttons.
//
// The agent is reached at an address: a Unix socket, or on Windows a
// named pipe (\\.\pipe\...), "" for the default (DefaultAgentAddr). Dial
// keeps the agent's connection open while it logs in, and closes it
// once logged in: the keys are tried one after the other, as OpenSSH
// does, and each signature may wait for the user to approve it in the
// agent, which no timeout cuts short.

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// Errors of an SSH agent, which AgentError wraps.
var (
	// ErrNoAgentAddr is no address given and no default one: on macOS
	// and Linux, SSH_AUTH_SOCK is not set.
	ErrNoAgentAddr = errors.New("no SSH agent address, and SSH_AUTH_SOCK is not set")
	// ErrAgentNoKeys is an agent that holds no keys.
	ErrAgentNoKeys = errors.New("the SSH agent has no keys")
	// ErrAgentRefused is a server that refused every key of the agent.
	ErrAgentRefused = errors.New("the server refused every key in the SSH agent")
	// ErrAgentSign is an agent that did not sign the login: the user
	// denied it, or the agent failed.
	ErrAgentSign = errors.New("the SSH agent did not sign")
)

// AgentError is a login with an SSH agent that failed: Err is one of
// ErrNoAgentAddr, ErrAgentNoKeys, ErrAgentRefused or ErrAgentSign, or why
// the agent at Addr could not be reached.
type AgentError struct {
	Addr string // the agent's address, "" with ErrNoAgentAddr
	Err  error
}

func (e *AgentError) Error() string {
	if e.Addr == "" {
		return e.Err.Error()
	}
	return "SSH agent " + e.Addr + ": " + e.Err.Error()
}

func (e *AgentError) Unwrap() error { return e.Err }

// AgentAddr is the address of the agent that setting names: setting
// itself, else the default.
func AgentAddr(setting string) string {
	if s := strings.TrimSpace(setting); s != "" {
		return s
	}
	return DefaultAgentAddr()
}

// AgentKey is a key an agent holds, as `ssh-add -l` shows it.
type AgentKey struct {
	Type        string // "ED25519", "RSA 4096", "ECDSA 256"
	Fingerprint string // SHA256:...
	Comment     string
}

// dialAgent connects to the agent setting names. Closing the agent's
// connection is the caller's.
func dialAgent(setting string) (agent.ExtendedAgent, io.Closer, string, error) {
	addr := AgentAddr(setting)
	if addr == "" {
		return nil, nil, "", &AgentError{Err: ErrNoAgentAddr}
	}
	conn, err := openAgent(ExpandHome(addr))
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) {
			err = op.Err // not "dial unix <the address>" again
		}
		return nil, nil, addr, &AgentError{Addr: addr, Err: err}
	}
	return agent.NewClient(conn), conn, addr, nil
}

// dialUnix connects to an agent's Unix socket.
func dialUnix(path string) (io.ReadWriteCloser, error) { return net.Dial("unix", path) }

// ListAgentKeys connects to the agent setting names and lists its keys,
// with the address it used. An agent with none is no error. Listing
// asks no agent to approve anything: only signing does.
func ListAgentKeys(setting string) (string, []AgentKey, error) {
	ag, conn, addr, err := dialAgent(setting)
	if err != nil {
		return addr, nil, err
	}
	defer conn.Close()
	keys, err := ag.List()
	if err != nil {
		return addr, nil, &AgentError{Addr: addr, Err: err}
	}
	out := make([]AgentKey, len(keys))
	for i, k := range keys {
		var pub ssh.PublicKey = k
		if p, err := ssh.ParsePublicKey(k.Blob); err == nil {
			pub = p // for the size of an RSA key
		}
		out[i] = AgentKey{Type: keyTypeLabel(pub), Fingerprint: ssh.FingerprintSHA256(k), Comment: k.Comment}
	}
	return addr, out, nil
}

// keyTypeLabel names a key's type as ssh-add -l does: "ED25519",
// "RSA 4096", "ECDSA 256".
func keyTypeLabel(k ssh.PublicKey) string {
	t := k.Type()
	cert := strings.HasSuffix(t, "-cert-v01@openssh.com")
	if cert {
		t = strings.TrimSuffix(t, "-cert-v01@openssh.com")
		if c, ok := k.(*ssh.Certificate); ok {
			k = c.Key
		}
	}
	var label string
	switch {
	case t == ssh.KeyAlgoED25519:
		label = "ED25519"
	case t == ssh.KeyAlgoSKED25519:
		label = "ED25519-SK"
	case t == ssh.KeyAlgoSKECDSA256:
		label = "ECDSA-SK"
	case strings.HasPrefix(t, "ecdsa-sha2-nistp"):
		label = "ECDSA " + strings.TrimPrefix(t, "ecdsa-sha2-nistp")
	case t == ssh.KeyAlgoRSA:
		label = "RSA"
		if ck, ok := k.(ssh.CryptoPublicKey); ok {
			if rk, ok := ck.CryptoPublicKey().(*rsa.PublicKey); ok {
				label += " " + strconv.Itoa(rk.N.BitLen())
			}
		}
	case t == ssh.InsecureKeyAlgoDSA:
		label = "DSA"
	default:
		label = t
	}
	if cert {
		label += "-CERT"
	}
	return label
}

// agentLogin is what logging in with an agent holds until Dial is done:
// the agent's connection.
type agentLogin struct {
	addr string
	conn io.Closer
}

// agentAuth connects to the agent setting names, and returns the method
// that logs in with its keys, one after the other. It fails when the
// agent cannot be reached or has no keys.
func agentAuth(setting string) (ssh.AuthMethod, *agentLogin, error) {
	ag, conn, addr, err := dialAgent(setting)
	if err != nil {
		return nil, nil, err
	}
	keys, err := ag.List()
	if err == nil && len(keys) == 0 {
		err = ErrAgentNoKeys
	}
	if err != nil {
		conn.Close()
		return nil, nil, &AgentError{Addr: addr, Err: err}
	}
	method := ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
		signers, err := ag.Signers()
		if err != nil {
			return nil, &AgentError{Addr: addr, Err: err}
		}
		for i, s := range signers {
			if as, ok := s.(ssh.AlgorithmSigner); ok {
				signers[i] = agentSigner{as, addr}
			}
		}
		return signers, nil
	})
	return method, &agentLogin{addr: addr, conn: conn}, nil
}

// agentSigner is a key of the agent, whose failures to sign say so.
type agentSigner struct {
	ssh.AlgorithmSigner
	addr string
}

func (s agentSigner) Sign(rand io.Reader, data []byte) (*ssh.Signature, error) {
	sig, err := s.AlgorithmSigner.Sign(rand, data)
	return sig, s.signError(err)
}

func (s agentSigner) SignWithAlgorithm(rand io.Reader, data []byte, algorithm string) (*ssh.Signature, error) {
	sig, err := s.AlgorithmSigner.SignWithAlgorithm(rand, data, algorithm)
	return sig, s.signError(err)
}

func (s agentSigner) signError(err error) error {
	if err == nil {
		return nil
	}
	return &AgentError{Addr: s.addr, Err: fmt.Errorf("%w: %v", ErrAgentSign, err)}
}

// refused turns err, the failure of logging in to a host with the
// agent's keys, into ErrAgentRefused when the server refused them all;
// failures of a hop on the way, or of the agent, are left as they are.
func (l *agentLogin) refused(err error) error {
	var hop *HopError
	var ae *AgentError
	if err == nil || errors.As(err, &hop) || errors.As(err, &ae) || Classify(err) != "auth" {
		return err
	}
	return &AgentError{Addr: l.addr, Err: ErrAgentRefused}
}
