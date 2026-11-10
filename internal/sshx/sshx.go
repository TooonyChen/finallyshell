// Package sshx connects to SSH servers: it logs in, checks host keys
// against the app's known_hosts file, and opens the interactive shell the
// terminal shows.
package sshx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"finallyshell/internal/config"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Errors that say what went wrong, for the app to put in the user's words.
var (
	ErrHostKeyRejected = errors.New("host key rejected")
	ErrHostKeyTimeout  = errors.New("timed out waiting for the host key to be confirmed")
	ErrNeedPassphrase  = errors.New("the private key needs a passphrase")
	ErrNoAuth          = errors.New("no authentication method")
)

// HostKeyChangedError is a host whose key is not the one known_hosts has.
type HostKeyChangedError struct {
	Fingerprint string
	File        string
}

func (e *HostKeyChangedError) Error() string {
	return fmt.Sprintf("host key changed (%s); remove the old entry from %s if the server was reinstalled", e.Fingerprint, e.File)
}

// KeyFileError is a private key that could not be read (Parse false) or
// parsed (Parse true).
type KeyFileError struct {
	Parse bool
	Err   error
}

func (e *KeyFileError) Error() string { return "private key: " + e.Err.Error() }
func (e *KeyFileError) Unwrap() error { return e.Err }

// HostKeyQuestion asks the user whether to trust a host key not seen
// before. The connecting goroutine waits for one value on Answer.
type HostKeyQuestion struct {
	Host        string
	KeyType     string
	Fingerprint string
	Answer      chan bool
}

// Options is what Dial needs besides the host.
type Options struct {
	Password   string
	Passphrase string
	// AskHostKey shows q and returns at once; the answer comes on q.Answer.
	AskHostKey func(q *HostKeyQuestion)
	// KnownHosts is the known_hosts file: <config dir>/known_hosts when
	// empty.
	KnownHosts string

	// ProxyPassword is the password of the host's SOCKS5 or HTTP proxy,
	// for the proxy's user (see proxy.go).
	ProxyPassword string
	// Jump is the jump host of a host whose proxy is one, with what
	// logging in to it takes, its own jump host included.
	Jump *Hop

	// Agent is the address of the SSH agent a host that logs in with one
	// (config.AuthAgent) uses: "" for the default, DefaultAgentAddr.
	Agent string

	// KeepAlive is the keepalive Dial starts on the client it returns,
	// and on the client of each jump host on the way, whose own is not
	// used: each runs as long as its client is open (see KeepAlive).
	KeepAlive KeepAliveConfig
}

var knownHostsMu sync.Mutex

func hostKeyCallback(opts Options) (ssh.HostKeyCallback, error) {
	path := opts.KnownHosts
	if path == "" {
		path = filepath.Join(config.Dir(), "known_hosts")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	check, err := knownhosts.New(path)
	if err != nil {
		return nil, err
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := check(hostname, remote, key)
		var keyErr *knownhosts.KeyError
		if err == nil || !errors.As(err, &keyErr) {
			return err
		}
		if len(keyErr.Want) > 0 {
			return &HostKeyChangedError{Fingerprint: ssh.FingerprintSHA256(key), File: path}
		}
		if opts.AskHostKey == nil {
			return ErrHostKeyRejected
		}
		q := &HostKeyQuestion{Host: hostname, KeyType: key.Type(), Fingerprint: ssh.FingerprintSHA256(key), Answer: make(chan bool, 1)}
		opts.AskHostKey(q)
		select {
		case ok := <-q.Answer:
			if !ok {
				return ErrHostKeyRejected
			}
		case <-time.After(5 * time.Minute):
			return ErrHostKeyTimeout
		}
		knownHostsMu.Lock()
		defer knownHostsMu.Unlock()
		out, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = fmt.Fprintln(out, knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key))
		return err
	}, nil
}

// authMethods are the ways h logs in. With an agent, the agent's
// connection is open until the caller closes the agentLogin's.
func authMethods(h *config.Host, opts Options) ([]ssh.AuthMethod, *agentLogin, error) {
	if h.Auth == config.AuthAgent {
		method, login, err := agentAuth(opts.Agent)
		if err != nil {
			return nil, nil, err
		}
		return []ssh.AuthMethod{method}, login, nil
	}
	var methods []ssh.AuthMethod
	if h.Auth == config.AuthKey {
		data, err := os.ReadFile(ExpandHome(h.KeyPath))
		if err != nil {
			return nil, nil, &KeyFileError{Err: err}
		}
		signer, err := ssh.ParsePrivateKey(data)
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			if opts.Passphrase == "" {
				return nil, nil, ErrNeedPassphrase
			}
			signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(opts.Passphrase))
		}
		if err != nil {
			return nil, nil, &KeyFileError{Parse: true, Err: err}
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if opts.Password != "" {
		pw := opts.Password
		methods = append(methods, ssh.Password(pw),
			// Many servers only offer keyboard-interactive for passwords.
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = pw
				}
				return answers, nil
			}))
	}
	if len(methods) == 0 {
		return nil, nil, ErrNoAuth
	}
	return methods, nil, nil
}

// Dial connects and logs in to h, through its proxy or jump host if it
// has one (see proxy.go), and starts opts.KeepAlive on the connection.
//
// A host that logs in with an SSH agent keeps the agent's connection open
// until it is logged in, and no longer. Its errors are *AgentError: the
// agent missing, without keys, not signing, or every key refused.
func Dial(h *config.Host, opts Options) (*ssh.Client, error) {
	auth, agentLogin, err := authMethods(h, opts)
	if err != nil {
		return nil, err
	}
	if agentLogin != nil {
		defer agentLogin.conn.Close()
	}
	hk, err := hostKeyCallback(opts)
	if err != nil {
		return nil, err
	}
	// Timeout is the TCP connection's: logging in has none, as an agent
	// may wait for the user to approve each signature.
	cfg := &ssh.ClientConfig{User: h.User, Auth: auth, HostKeyCallback: hk, Timeout: 15 * time.Second}
	client, err := dialRoute(h, opts, cfg)
	if agentLogin != nil {
		err = agentLogin.refused(err)
	}
	if err == nil && opts.KeepAlive.Interval > 0 {
		go KeepAlive(context.Background(), client, opts.KeepAlive)
	}
	return client, err
}

// Shell is an interactive shell on an SSH connection, which the terminal
// view reads and writes.
type Shell struct {
	sess   *ssh.Session
	stdin  io.WriteCloser
	stdout io.Reader
}

// PTY is the pseudo-terminal a shell runs in.
type PTY struct {
	// Term is its type, the shell's TERM: config.DefaultTermType when
	// empty.
	Term string
	// Erase is the character the terminal's Backspace sends, which the
	// server's line editing then erases with (VERASE): DEL (0x7f) when 0.
	Erase byte
}

// OpenShell starts a login shell in a pseudo-terminal as pty says.
func OpenShell(client *ssh.Client, pty PTY) (*Shell, error) {
	term, erase := pty.Term, pty.Erase
	if term == "" {
		term = config.DefaultTermType
	}
	if erase == 0 {
		erase = 0x7f
	}
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		return nil, err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		return nil, err
	}
	sess.Stderr = sess.Stdout
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.VERASE: uint32(erase), ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if err := sess.RequestPty(term, 24, 80, modes); err != nil {
		sess.Close()
		return nil, err
	}
	sess.Setenv("COLORTERM", "truecolor") // most servers refuse it; harmless
	if err := sess.Shell(); err != nil {
		sess.Close()
		return nil, err
	}
	return &Shell{sess: sess, stdin: stdin, stdout: stdout}, nil
}

func (s *Shell) Read(p []byte) (int, error)  { return s.stdout.Read(p) }
func (s *Shell) Write(p []byte) (int, error) { return s.stdin.Write(p) }
func (s *Shell) Close() error                { return s.sess.Close() }

// Resize is called by the terminal view as its size in cells changes.
func (s *Shell) Resize(cols, rows int) error { return s.sess.WindowChange(rows, cols) }

// Classify names the network failure behind a Dial error, for errors
// that are not this package's: "auth", "refused", "timeout", "nohost", or
// "" for any other.
func Classify(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "unable to authenticate"):
		return "auth"
	case strings.Contains(msg, "connection refused"):
		return "refused"
	case strings.Contains(msg, "i/o timeout"):
		return "timeout"
	case strings.Contains(msg, "no such host"):
		return "nohost"
	}
	return ""
}

// ExpandHome replaces a leading ~ with the user's home directory.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}
