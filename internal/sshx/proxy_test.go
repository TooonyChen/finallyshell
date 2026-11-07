package sshx

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"finalshelly/internal/config"

	"golang.org/x/crypto/ssh"
)

// testServer is an SSH server in the test that takes the password
// "secret", answers "ping" requests, and forwards direct-tcpip channels
// when forward is set.
type testServer struct {
	addr    string
	host    string
	port    int
	forward bool
	open    atomic.Int32 // connections logged in and not closed yet
}

func startServer(t *testing.T, forward bool) *testServer {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if string(pw) != "secret" {
			return nil, errors.New("wrong password")
		}
		return nil, nil
	}}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	srv := &testServer{addr: ln.Addr().String(), forward: forward}
	srv.host, srv.port = splitAddr(t, srv.addr)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.serve(conn, cfg)
		}
	}()
	return srv
}

func (srv *testServer) serve(conn net.Conn, cfg *ssh.ServerConfig) {
	sconn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		conn.Close()
		return
	}
	srv.open.Add(1)
	defer srv.open.Add(-1)
	defer sconn.Close()
	go func() {
		for r := range reqs {
			r.Reply(r.Type == "ping", nil)
		}
	}()
	for nc := range chans {
		if nc.ChannelType() != "direct-tcpip" || !srv.forward {
			nc.Reject(ssh.Prohibited, "no")
			continue
		}
		var to struct {
			Host       string
			Port       uint32
			OriginHost string
			OriginPort uint32
		}
		if err := ssh.Unmarshal(nc.ExtraData(), &to); err != nil {
			nc.Reject(ssh.ConnectionFailed, "bad request")
			continue
		}
		out, err := net.Dial("tcp", net.JoinHostPort(to.Host, strconv.Itoa(int(to.Port))))
		if err != nil {
			nc.Reject(ssh.ConnectionFailed, "Connection refused")
			continue
		}
		ch, chReqs, err := nc.Accept()
		if err != nil {
			out.Close()
			continue
		}
		go ssh.DiscardRequests(chReqs)
		go pipe(ch, out)
	}
}

// pipe copies a to b and b to a until either ends, then closes both.
func pipe(a, b io.ReadWriteCloser) {
	var once sync.Once
	done := func() { once.Do(func() { a.Close(); b.Close() }) }
	go func() { io.Copy(a, b); done() }()
	io.Copy(b, a)
	done()
}

func splitAddr(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(port)
	return host, n
}

// closedPort is a local port nothing listens on.
func closedPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// testOptions logs in with pw, trusting every host key and keeping the
// hosts asked about.
func testOptions(t *testing.T, pw string, asked *[]string) Options {
	known := filepath.Join(t.TempDir(), "known_hosts")
	var mu sync.Mutex
	return Options{Password: pw, KnownHosts: known, AskHostKey: func(q *HostKeyQuestion) {
		mu.Lock()
		*asked = append(*asked, q.Host)
		mu.Unlock()
		q.Answer <- true
	}}
}

// ping checks that the SSH connection works end to end.
func ping(t *testing.T, c *ssh.Client) {
	t.Helper()
	ok, _, err := c.SendRequest("ping", true, nil)
	if err != nil || !ok {
		t.Fatalf("ping: %v %v", ok, err)
	}
}

// socksServer is a SOCKS5 proxy in the test: with a user, it asks for
// the user and password. It resolves the name ssh.internal, which only
// it knows, to 127.0.0.1.
type socksServer struct {
	addr, host string
	port       int
	user, pw   string
	asked      atomic.Value // the last address asked for
}

func startSOCKS(t *testing.T, user, pw string) *socksServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	s := &socksServer{addr: ln.Addr().String(), user: user, pw: pw}
	s.host, s.port = splitAddr(t, s.addr)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s
}

func (s *socksServer) serve(c net.Conn) {
	r := bufio.NewReader(c)
	fail := func() { c.Close() }
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(r, hdr); err != nil || hdr[0] != 5 {
		fail()
		return
	}
	methods := make([]byte, hdr[1])
	io.ReadFull(r, methods)
	want := byte(0)
	if s.user != "" {
		want = 2
	}
	if !slices.Contains(methods, want) {
		c.Write([]byte{5, 0xff})
		fail()
		return
	}
	c.Write([]byte{5, want})
	if want == 2 {
		b := make([]byte, 2)
		io.ReadFull(r, b)
		user := make([]byte, b[1])
		io.ReadFull(r, user)
		n, _ := r.ReadByte()
		pw := make([]byte, n)
		io.ReadFull(r, pw)
		if string(user) != s.user || string(pw) != s.pw {
			c.Write([]byte{1, 1})
			fail()
			return
		}
		c.Write([]byte{1, 0})
	}
	req := make([]byte, 4)
	if _, err := io.ReadFull(r, req); err != nil || req[1] != 1 {
		fail()
		return
	}
	var host string
	switch req[3] {
	case 1:
		ip := make([]byte, 4)
		io.ReadFull(r, ip)
		host = net.IP(ip).String()
	case 3:
		n, _ := r.ReadByte()
		name := make([]byte, n)
		io.ReadFull(r, name)
		host = string(name)
	default:
		fail()
		return
	}
	pb := make([]byte, 2)
	io.ReadFull(r, pb)
	addr := net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(pb))))
	s.asked.Store(addr)
	if host == "ssh.internal" { // a name only the proxy resolves
		addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(int(binary.BigEndian.Uint16(pb))))
	}
	out, err := net.Dial("tcp", addr)
	if err != nil {
		c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0}) // connection refused
		fail()
		return
	}
	c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
	pipe(struct {
		io.Reader
		io.WriteCloser
	}{r, c}, out)
}

// httpProxy is an HTTP proxy in the test that answers CONNECT, asking
// for basic authentication with a user.
type httpProxy struct {
	host     string
	port     int
	user, pw string
}

func startHTTPProxy(t *testing.T, user, pw string) *httpProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	p := &httpProxy{user: user, pw: pw}
	p.host, p.port = splitAddr(t, ln.Addr().String())
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go p.serve(c)
		}
	}()
	return p
}

func (p *httpProxy) serve(c net.Conn) {
	r := bufio.NewReader(c)
	req, err := http.ReadRequest(r)
	if err != nil || req.Method != http.MethodConnect {
		c.Close()
		return
	}
	if p.user != "" {
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte(p.user+":"+p.pw))
		if req.Header.Get("Proxy-Authorization") != want {
			io.WriteString(c, "HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic realm=\"test\"\r\nContent-Length: 0\r\n\r\n")
			c.Close()
			return
		}
	}
	out, err := net.Dial("tcp", req.Host)
	if err != nil {
		io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		c.Close()
		return
	}
	// The answer and the server's first bytes in one write, as a proxy
	// may send them: the client must not lose what comes after the
	// answer.
	banner := make([]byte, 256)
	out.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, _ := out.Read(banner)
	out.SetReadDeadline(time.Time{})
	c.Write(append([]byte("HTTP/1.1 200 Connection established\r\n\r\n"), banner[:n]...))
	pipe(struct {
		io.Reader
		io.WriteCloser
	}{r, c}, out)
}

func target(srv *testServer, p *config.ProxyConfig) *config.Host {
	return &config.Host{ID: "target", Name: "target", Addr: srv.host, Port: srv.port, User: "u", Auth: config.AuthPassword, Proxy: p}
}

func TestDialSOCKS5(t *testing.T) {
	srv := startServer(t, false)
	open := startSOCKS(t, "", "")
	authed := startSOCKS(t, "alice", "pa55")
	for _, c := range []struct {
		name  string
		proxy *socksServer
		user  string
		pw    string
	}{
		{"no auth", open, "", ""},
		{"auth", authed, "alice", "pa55"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var asked []string
			opts := testOptions(t, "secret", &asked)
			opts.ProxyPassword = c.pw
			h := target(srv, &config.ProxyConfig{Kind: config.ProxySOCKS5, Addr: c.proxy.host, Port: c.proxy.port, User: c.user})
			client, err := Dial(h, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			ping(t, client)
			// The host key is the host's, not the proxy's, and so is the
			// address the app shows.
			if !slices.Equal(asked, []string{srv.addr}) {
				t.Errorf("asked about %v", asked)
			}
			if addr, ok := client.RemoteAddr().(*net.TCPAddr); !ok || addr.String() != srv.addr {
				t.Errorf("remote address %v", client.RemoteAddr())
			}
		})
	}

	// The proxy resolves the name.
	var asked []string
	h := target(srv, &config.ProxyConfig{Kind: config.ProxySOCKS5, Addr: open.host, Port: open.port})
	h.Addr = "ssh.internal"
	client, err := Dial(h, testOptions(t, "secret", &asked))
	if err != nil {
		t.Fatal(err)
	}
	ping(t, client)
	client.Close()
	if want := net.JoinHostPort("ssh.internal", strconv.Itoa(srv.port)); open.asked.Load() != want || client.RemoteAddr().String() != want {
		t.Errorf("asked %v, remote %v", open.asked.Load(), client.RemoteAddr())
	}

	t.Run("failures", func(t *testing.T) {
		paddr := authed.addr
		for _, c := range []struct {
			name   string
			proxy  config.ProxyConfig
			pw     string
			target *testServer
			check  func(err error) bool
		}{
			{"wrong password", config.ProxyConfig{Addr: authed.host, Port: authed.port, User: "alice"}, "nope", srv,
				func(err error) bool { return errors.Is(err, ErrProxyAuth) }},
			{"no user", config.ProxyConfig{Addr: authed.host, Port: authed.port}, "", srv,
				func(err error) bool { return errors.Is(err, ErrProxyAuth) }},
			{"target refused", config.ProxyConfig{Addr: authed.host, Port: authed.port, User: "alice"}, "pa55", &testServer{host: "127.0.0.1", port: closedPort(t)},
				func(err error) bool {
					var te *ProxyTargetError
					return errors.As(err, &te) && te.Reason == "connection refused"
				}},
		} {
			t.Run(c.name, func(t *testing.T) {
				var asked []string
				opts := testOptions(t, "secret", &asked)
				opts.ProxyPassword = c.pw
				c.proxy.Kind = config.ProxySOCKS5
				_, err := Dial(target(c.target, &c.proxy), opts)
				var hop *HopError
				if !errors.As(err, &hop) || hop.Proxy != paddr || hop.HostID != "target" || !c.check(hop.Err) {
					t.Fatalf("error %v", err)
				}
				if !strings.HasPrefix(err.Error(), "proxy "+paddr+": ") {
					t.Errorf("message %q", err)
				}
			})
		}
	})

	t.Run("proxy down", func(t *testing.T) {
		var asked []string
		port := closedPort(t)
		_, err := Dial(target(srv, &config.ProxyConfig{Kind: config.ProxySOCKS5, Addr: "127.0.0.1", Port: port}), testOptions(t, "secret", &asked))
		var hop *HopError
		if !errors.As(err, &hop) || hop.Proxy != "127.0.0.1:"+strconv.Itoa(port) || Classify(hop.Err) != "refused" {
			t.Fatalf("error %v", err)
		}
	})
}

func TestDialHTTPProxy(t *testing.T) {
	srv := startServer(t, false)
	open := startHTTPProxy(t, "", "")
	authed := startHTTPProxy(t, "bob", "s3cr:et")
	for _, c := range []struct {
		name  string
		proxy *httpProxy
		user  string
		pw    string
	}{
		{"no auth", open, "", ""},
		{"basic auth", authed, "bob", "s3cr:et"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var asked []string
			opts := testOptions(t, "secret", &asked)
			opts.ProxyPassword = c.pw
			client, err := Dial(target(srv, &config.ProxyConfig{Kind: config.ProxyHTTP, Addr: c.proxy.host, Port: c.proxy.port, User: c.user}), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			ping(t, client)
			if !slices.Equal(asked, []string{srv.addr}) {
				t.Errorf("asked about %v", asked)
			}
		})
	}

	paddr := net.JoinHostPort(authed.host, strconv.Itoa(authed.port))
	for _, c := range []struct {
		name  string
		user  string
		pw    string
		port  int
		check func(err error) bool
	}{
		{"wrong password", "bob", "nope", srv.port, func(err error) bool { return errors.Is(err, ErrProxyAuth) }},
		{"no user", "", "", srv.port, func(err error) bool { return errors.Is(err, ErrProxyAuth) }},
		{"bad gateway", "bob", "s3cr:et", closedPort(t), func(err error) bool {
			var te *ProxyTargetError
			return errors.As(err, &te) && te.Reason == "502 Bad Gateway"
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var asked []string
			opts := testOptions(t, "secret", &asked)
			opts.ProxyPassword = c.pw
			h := target(srv, &config.ProxyConfig{Kind: config.ProxyHTTP, Addr: authed.host, Port: authed.port, User: c.user})
			h.Port = c.port
			_, err := Dial(h, opts)
			var hop *HopError
			if !errors.As(err, &hop) || hop.Proxy != paddr || hop.HostID != "target" || !c.check(hop.Err) {
				t.Fatalf("error %v", err)
			}
		})
	}

	// A proxy that never answers times out.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	host, port := splitAddr(t, ln.Addr().String())
	_, err = dialHTTP(&config.ProxyConfig{Kind: config.ProxyHTTP, Addr: host, Port: port}, "", srv.addr, 200*time.Millisecond)
	var hop *HopError
	if !errors.As(err, &hop) || Classify(hop.Err) != "timeout" {
		t.Errorf("silent proxy: %v", err)
	}
}

func TestDialJumpHost(t *testing.T) {
	jump := startServer(t, true)
	srv := startServer(t, false)
	jumpHost := &config.Host{ID: "j", Name: "jump-1", Addr: jump.host, Port: jump.port, User: "j", Auth: config.AuthPassword}
	h := target(srv, &config.ProxyConfig{Kind: config.ProxyJump, JumpHost: "j"})

	var asked []string
	opts := testOptions(t, "secret", &asked)
	jumpOpts := opts
	opts.Jump = &Hop{Host: jumpHost, Options: jumpOpts}
	client, err := Dial(h, opts)
	if err != nil {
		t.Fatal(err)
	}
	ping(t, client)
	// Both host keys are checked: the jump host's first.
	if !slices.Equal(asked, []string{jump.addr, srv.addr}) {
		t.Errorf("asked about %v", asked)
	}
	if client.RemoteAddr().String() != srv.addr {
		t.Errorf("remote %v", client.RemoteAddr())
	}
	if jump.open.Load() != 1 || srv.open.Load() != 1 {
		t.Fatalf("open: jump %d, target %d", jump.open.Load(), srv.open.Load())
	}
	// Closing the host's client closes the jump host's.
	client.Close()
	waitFor(t, "the jump host's connection to close", func() bool { return jump.open.Load() == 0 && srv.open.Load() == 0 })

	// Known now: nothing asked the second time.
	asked = nil
	client, err = Dial(h, opts)
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
	if len(asked) != 0 {
		t.Errorf("asked again about %v", asked)
	}

	t.Run("wrong password", func(t *testing.T) {
		bad := opts
		badJump := jumpOpts
		badJump.Password = "nope"
		bad.Jump = &Hop{Host: jumpHost, Options: badJump}
		_, err := Dial(h, bad)
		var hop *HopError
		if !errors.As(err, &hop) || hop.JumpHost != "jump-1" || hop.HostID != "j" || hop.Proxy != "" || Classify(hop.Err) != "auth" {
			t.Fatalf("error %v", err)
		}
		if !strings.HasPrefix(err.Error(), "jump host jump-1: ") {
			t.Errorf("message %q", err)
		}
	})

	t.Run("forwarding refused", func(t *testing.T) {
		noFwd := startServer(t, false)
		jh := *jumpHost
		jh.Port = noFwd.port
		o := opts
		o.Jump = &Hop{Host: &jh, Options: jumpOpts}
		_, err := Dial(h, o)
		var hop *HopError
		var fwd *ForwardError
		if !errors.As(err, &hop) || hop.JumpHost != "jump-1" || hop.HostID != "j" || !errors.As(err, &fwd) || fwd.Addr != srv.addr {
			t.Fatalf("error %v", err)
		}
		if noFwd.open.Load() != 0 {
			waitFor(t, "the jump host closed", func() bool { return noFwd.open.Load() == 0 })
		}
	})

	t.Run("target down", func(t *testing.T) {
		down := *h
		down.Port = closedPort(t)
		_, err := Dial(&down, opts)
		var fwd *ForwardError
		if !errors.As(err, &fwd) || !strings.Contains(fwd.Err.Error(), "Connection refused") {
			t.Fatalf("error %v", err)
		}
		waitFor(t, "the jump host closed", func() bool { return jump.open.Load() == 0 })
	})

	t.Run("no jump host", func(t *testing.T) {
		if _, err := Dial(h, testOptions(t, "secret", new([]string))); !errors.Is(err, ErrNoJumpHost) {
			t.Fatalf("error %v", err)
		}
	})

	// A chain: the target through a jump host, reached through a
	// SOCKS5 proxy, through another jump host.
	t.Run("chain", func(t *testing.T) {
		socks := startSOCKS(t, "", "")
		outer := startServer(t, true)
		outerHost := &config.Host{ID: "o", Name: "outer", Addr: outer.host, Port: outer.port, User: "o", Auth: config.AuthPassword,
			Proxy: &config.ProxyConfig{Kind: config.ProxySOCKS5, Addr: socks.host, Port: socks.port}}
		mid := *jumpHost
		mid.Proxy = &config.ProxyConfig{Kind: config.ProxyJump, JumpHost: "o"}
		var asked []string
		o := testOptions(t, "secret", &asked)
		midOpts := o
		midOpts.Jump = &Hop{Host: outerHost, Options: o}
		o.Jump = &Hop{Host: &mid, Options: midOpts}
		client, err := Dial(h, o)
		if err != nil {
			t.Fatal(err)
		}
		ping(t, client)
		if !slices.Equal(asked, []string{outer.addr, jump.addr, srv.addr}) {
			t.Errorf("asked about %v", asked)
		}
		client.Close()
		waitFor(t, "every hop closed", func() bool { return outer.open.Load() == 0 && jump.open.Load() == 0 && srv.open.Load() == 0 })

		// The proxy at the start of the chain fails: the error names it,
		// not the jump hosts.
		socksDown := *outerHost
		port := closedPort(t)
		socksDown.Proxy = &config.ProxyConfig{Kind: config.ProxySOCKS5, Addr: "127.0.0.1", Port: port}
		midOpts.Jump = &Hop{Host: &socksDown, Options: o}
		o.Jump = &Hop{Host: &mid, Options: midOpts}
		_, err = Dial(h, o)
		var hop *HopError
		if !errors.As(err, &hop) || hop.Proxy != "127.0.0.1:"+strconv.Itoa(port) || hop.HostID != "o" || !strings.HasPrefix(err.Error(), "proxy ") {
			t.Fatalf("error %v", err)
		}
	})
}

// TestJumpHostLive goes through the server FINALSHELLY_TEST_SSH names to
// itself: the server is its own jump host. It needs TCP forwarding on.
func TestJumpHostLive(t *testing.T) {
	spec := os.Getenv("FINALSHELLY_TEST_SSH")
	if spec == "" {
		t.Skip("FINALSHELLY_TEST_SSH not set")
	}
	cred, addr, _ := strings.Cut(spec, "@")
	user, pass, _ := strings.Cut(cred, ":")
	host, port := splitAddr(t, addr)
	jumpHost := &config.Host{ID: "j", Name: "live-jump", Addr: host, Port: port, User: user, Auth: config.AuthPassword}
	h := &config.Host{ID: "t", Name: "live", Addr: host, Port: port, User: user, Auth: config.AuthPassword,
		Proxy: &config.ProxyConfig{Kind: config.ProxyJump, JumpHost: "j"}}
	var asked []string
	opts := testOptions(t, pass, &asked)
	opts.Jump = &Hop{Host: jumpHost, Options: opts}
	client, err := Dial(h, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	out, err := sess.Output("echo through-$((6*7))")
	if err != nil || strings.TrimSpace(string(out)) != "through-42" {
		t.Fatalf("output %q %v", out, err)
	}
	// The same host twice: asked once, as the second finds it known.
	if len(asked) != 1 || asked[0] != addr {
		t.Errorf("asked about %v", asked)
	}

	// The jump host's password is wrong.
	bad := opts
	jo := opts
	jo.Password = pass + "-wrong"
	bad.Jump = &Hop{Host: jumpHost, Options: jo}
	_, err = Dial(h, bad)
	var hop *HopError
	if !errors.As(err, &hop) || hop.JumpHost != "live-jump" || Classify(hop.Err) != "auth" {
		t.Errorf("wrong jump password: %v", err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
