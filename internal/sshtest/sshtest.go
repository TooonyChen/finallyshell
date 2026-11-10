// Package sshtest is an SSH server for tests, in the test's process: it
// logs in with a password, or with the public keys AcceptKey gives it, opens shells that read and print nothing,
// keeping what they read, forwards ports as OpenSSH does with AllowTcpForwarding on and
// GatewayPorts off, answers keepalives, and, started with StartSFTP,
// serves the local file system over SFTP.
package sshtest

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Password is the password the server takes, for any user.
const Password = "secret"

// Server is a running test server.
type Server struct {
	// Host and Port are where it listens, on the loopback.
	Host string
	Port int

	ln  net.Listener
	cfg *ssh.ServerConfig
	// sftpDir is the SFTP subsystem's working directory, the user's home;
	// "" turns the subsystem off.
	sftpDir string

	mu    sync.Mutex
	conns map[*ssh.ServerConn]bool
	// forwards are the server's listeners for tcpip-forward, by the
	// connection that asked and the address it was asked for.
	forwards map[*ssh.ServerConn]map[string]net.Listener
	wg       sync.WaitGroup

	keepAlives atomic.Int64 // keepalive@openssh.com requests received
	logins     atomic.Int64 // passwords taken
	// keys are the public keys it logs in with, by their wire form, and
	// keyLogins the SHA256 fingerprints of those logged in with, in
	// order, under mu.
	keys      map[string]bool
	keyLogins []string
	// maxSessions refuses session channels past that many open on a
	// connection, as OpenSSH's MaxSessions does: 0 for no limit.
	maxSessions atomic.Int64
	shells      []*shell    // the shells opened, in order, under mu
	silent      atomic.Bool // keepalives go unanswered
	hangExec    atomic.Bool // exec requests are accepted and never run
}

// Start starts a server, which stops as the test ends, with every
// connection it has. It refuses SFTP, as servers without it do.
func Start(t testing.TB) *Server {
	t.Helper()
	return start(t, "")
}

// StartSFTP starts a server as Start does, which also serves the local
// file system over SFTP, as the user running the test, with home as the
// working directory.
func StartSFTP(t testing.TB, home string) *Server {
	t.Helper()
	return start(t, home)
}

func start(t testing.TB, sftpDir string) *Server {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{sftpDir: sftpDir, conns: map[*ssh.ServerConn]bool{}, forwards: map[*ssh.ServerConn]map[string]net.Listener{}}
	cfg := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if string(pw) != Password {
			return nil, errors.New("wrong password")
		}
		s.logins.Add(1)
		return nil, nil
	}}
	// Asked once whether a key would do, and again with its signature.
	cfg.PublicKeyCallback = func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		s.mu.Lock()
		ok := s.keys[string(key.Marshal())]
		s.mu.Unlock()
		if !ok {
			return nil, errors.New("unknown key")
		}
		return &ssh.Permissions{Extensions: map[string]string{"key": ssh.FingerprintSHA256(key)}}, nil
	}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.ln, s.cfg = ln, cfg
	addr := ln.Addr().(*net.TCPAddr)
	s.Host, s.Port = addr.IP.String(), addr.Port
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				s.serve(conn)
			}()
		}
	}()
	t.Cleanup(s.Close)
	return s
}

// Addr is host:port.
func (s *Server) Addr() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.Port)) }

// Dial logs in to the server as a client, accepting its key.
func (s *Server) Dial(t testing.TB) *ssh.Client {
	t.Helper()
	client, err := ssh.Dial("tcp", s.Addr(), &ssh.ClientConfig{
		User: "tester", Auth: []ssh.AuthMethod{ssh.Password(Password)}, HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// DropConnections closes every client's connection, as a network that
// fails.
func (s *Server) DropConnections() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		c.Close()
	}
}

// KeepAlives is how many keepalive@openssh.com requests the server has
// received, from every client.
func (s *Server) KeepAlives() int { return int(s.keepAlives.Load()) }

// StopAnswering leaves the keepalives that come from now on without an
// answer, as a server that hangs, or a network that lost the way back,
// while connections stay open.
func (s *Server) StopAnswering() { s.silent.Store(true) }

// HangExec accepts the exec requests that come from now on and runs
// nothing: the command never prints, ends or exits, as a script stuck on
// a stale mount. By default exec is refused.
func (s *Server) HangExec() { s.hangExec.Store(true) }

// AcceptKey lets any user log in with the private key of key.
func (s *Server) AcceptKey(key ssh.PublicKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keys == nil {
		s.keys = map[string]bool{}
	}
	s.keys[string(key.Marshal())] = true
}

// KeyLogins are the SHA256 fingerprints of the keys clients logged in
// with, in order.
func (s *Server) KeyLogins() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.keyLogins...)
}

// Logins is how many times a client logged in with the password, on
// every connection.
func (s *Server) Logins() int { return int(s.logins.Load()) }

// LimitSessions refuses the session channels (shells, exec, SFTP) a
// connection opens from now on past n open at once, as OpenSSH's
// MaxSessions; 0 lifts the limit.
func (s *Server) LimitSessions(n int) { s.maxSessions.Store(int64(n)) }

// shell is a shell the server opened: what it read, and its channel.
type shell struct {
	ch ssh.Channel
	mu sync.Mutex
	in bytes.Buffer
}

func (sh *shell) Write(p []byte) (int, error) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return sh.in.Write(p)
}

// Shells is how many shells the server opened, on every connection.
func (s *Server) Shells() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.shells)
}

// shell is the i-th shell opened, from 0.
func (s *Server) shell(i int) *shell {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i < 0 || i >= len(s.shells) {
		return nil
	}
	return s.shells[i]
}

// ShellInput is what the i-th shell opened, from 0, has read so far.
func (s *Server) ShellInput(i int) string {
	sh := s.shell(i)
	if sh == nil {
		return ""
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return sh.in.String()
}

// EndShell ends the i-th shell opened, from 0, as a shell that exits:
// exit status 0, then the channel's close. The connection stays.
func (s *Server) EndShell(i int) {
	if sh := s.shell(i); sh != nil {
		sh.ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
		sh.ch.Close()
	}
}

// Forwards is how many ports the server listens on for clients now.
func (s *Server) Forwards() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, f := range s.forwards {
		n += len(f)
	}
	return n
}

// Close stops the server and waits for its goroutines.
func (s *Server) Close() {
	s.ln.Close()
	s.DropConnections()
	s.wg.Wait()
}

func (s *Server) serve(conn net.Conn) {
	sconn, chans, reqs, err := ssh.NewServerConn(conn, s.cfg)
	if err != nil {
		conn.Close()
		return
	}
	if p := sconn.Permissions; p != nil && p.Extensions["key"] != "" {
		fp := p.Extensions["key"]
		s.mu.Lock()
		s.keyLogins = append(s.keyLogins, fp)
		s.mu.Unlock()
	}
	s.mu.Lock()
	s.conns[sconn] = true
	s.forwards[sconn] = map[string]net.Listener{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.conns, sconn)
		for _, ln := range s.forwards[sconn] {
			ln.Close()
		}
		delete(s.forwards, sconn)
		s.mu.Unlock()
	}()
	defer sconn.Close()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for r := range reqs {
			s.global(sconn, r)
		}
	}()
	var open atomic.Int64 // session channels open on the connection
	for nc := range chans {
		switch nc.ChannelType() {
		case "session":
			if max := s.maxSessions.Load(); max > 0 && open.Load() >= max {
				nc.Reject(ssh.Prohibited, "open failed") // as OpenSSH words it
				continue
			}
			open.Add(1)
			s.session(nc, func() { open.Add(-1) })
		case "direct-tcpip":
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				s.directTCPIP(nc)
			}()
		default:
			nc.Reject(ssh.UnknownChannelType, "no")
		}
	}
}

// session opens a shell that reads what comes and prints nothing, or
// the SFTP subsystem; closed runs once the channel closes.
func (s *Server) session(nc ssh.NewChannel, closed func()) {
	ch, reqs, err := nc.Accept()
	if err != nil {
		closed()
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer closed()
		started := false
		start := func(serve func()) {
			started = true
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				serve()
			}()
		}
		for r := range reqs {
			switch r.Type {
			case "pty-req", "window-change":
				r.Reply(true, nil)
			case "shell":
				r.Reply(!started, nil)
				if !started {
					sh := &shell{ch: ch}
					s.mu.Lock()
					s.shells = append(s.shells, sh)
					s.mu.Unlock()
					start(func() { io.Copy(sh, ch) })
				}
			case "subsystem":
				var name struct{ Name string }
				ok := s.sftpDir != "" && !started && ssh.Unmarshal(r.Payload, &name) == nil && name.Name == "sftp"
				r.Reply(ok, nil)
				if ok {
					start(func() { s.serveSFTP(ch) })
				}
			case "exec":
				hang := s.hangExec.Load() && !started
				r.Reply(hang, nil)
				started = started || hang
			default: // env
				r.Reply(false, nil)
			}
		}
	}()
}

// serveSFTP serves the local file system on ch until the client is done.
func (s *Server) serveSFTP(ch ssh.Channel) {
	defer ch.Close()
	srv, err := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(s.sftpDir))
	if err != nil {
		return
	}
	srv.Serve()
	srv.Close()
}

// directTCPIP connects to what the client asks for, as -L and -D need.
func (s *Server) directTCPIP(nc ssh.NewChannel) {
	var req struct {
		Host       string
		Port       uint32
		OriginHost string
		OriginPort uint32
	}
	if err := ssh.Unmarshal(nc.ExtraData(), &req); err != nil {
		nc.Reject(ssh.ConnectionFailed, "bad request")
		return
	}
	out, err := net.Dial("tcp", net.JoinHostPort(req.Host, strconv.Itoa(int(req.Port))))
	if err != nil {
		nc.Reject(ssh.ConnectionFailed, "Connection refused")
		return
	}
	ch, reqs, err := nc.Accept()
	if err != nil {
		out.Close()
		return
	}
	s.pipe(ch, reqs, out)
}

// global answers tcpip-forward and its cancel, as -R needs, and
// keepalives.
func (s *Server) global(sconn *ssh.ServerConn, r *ssh.Request) {
	var req struct {
		Addr string
		Port uint32
	}
	switch r.Type {
	case "tcpip-forward":
		if ssh.Unmarshal(r.Payload, &req) != nil {
			r.Reply(false, nil)
			return
		}
		// GatewayPorts no: the loopback, whatever is asked for.
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(req.Port))))
		if err != nil {
			r.Reply(false, nil)
			return
		}
		port := uint32(ln.Addr().(*net.TCPAddr).Port)
		key := net.JoinHostPort(req.Addr, strconv.Itoa(int(port)))
		s.mu.Lock()
		fwd := s.forwards[sconn]
		if fwd == nil { // the connection is gone
			s.mu.Unlock()
			ln.Close()
			r.Reply(false, nil)
			return
		}
		fwd[key] = ln
		s.mu.Unlock()
		if req.Port == 0 {
			r.Reply(true, ssh.Marshal(struct{ Port uint32 }{port}))
		} else {
			r.Reply(true, nil)
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				s.wg.Add(1)
				go func() {
					defer s.wg.Done()
					s.forwarded(sconn, req.Addr, port, c)
				}()
			}
		}()
	case "cancel-tcpip-forward":
		if ssh.Unmarshal(r.Payload, &req) != nil {
			r.Reply(false, nil)
			return
		}
		key := net.JoinHostPort(req.Addr, strconv.Itoa(int(req.Port)))
		s.mu.Lock()
		ln := s.forwards[sconn][key]
		delete(s.forwards[sconn], key)
		s.mu.Unlock()
		if ln != nil {
			ln.Close()
		}
		r.Reply(ln != nil, nil)
	case "keepalive@openssh.com":
		s.keepAlives.Add(1)
		if !s.silent.Load() {
			r.Reply(true, nil)
		}
	default:
		r.Reply(false, nil)
	}
}

// forwarded hands c, which connected to a forwarded port, to the client.
func (s *Server) forwarded(sconn *ssh.ServerConn, addr string, port uint32, c net.Conn) {
	origin := c.RemoteAddr().(*net.TCPAddr)
	payload := ssh.Marshal(struct {
		Addr       string
		Port       uint32
		OriginAddr string
		OriginPort uint32
	}{addr, port, origin.IP.String(), uint32(origin.Port)})
	ch, reqs, err := sconn.OpenChannel("forwarded-tcpip", payload)
	if err != nil {
		c.Close()
		return
	}
	s.pipe(ch, reqs, c)
}

// pipe copies both ways between ch and c, passing on the end of each
// direction, then closes both. Once the client closes ch, which ends its
// requests, and what it sent is passed on, c is closed too, for the copy
// that waits on it.
func (s *Server) pipe(ch ssh.Channel, reqs <-chan *ssh.Request, c net.Conn) {
	toC := make(chan struct{})
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		ssh.DiscardRequests(reqs)
		<-toC
		c.Close()
	}()
	go func() {
		defer close(toC)
		io.Copy(c, ch)
		if tc, ok := c.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()
	io.Copy(ch, c)
	ch.CloseWrite()
	<-toC
	ch.Close()
	c.Close()
	<-closed
}
