package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"finalshelly/internal/config"
	"finalshelly/internal/sshx"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"
)

// jumpSSH is an SSH server in the test that takes the password "secret"
// and forwards direct-tcpip channels, as a jump host.
type jumpSSH struct {
	host string
	port int
	open atomic.Int32 // connections logged in and not closed
}

func startJumpSSH(t *testing.T) *jumpSSH {
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
	srv := &jumpSSH{}
	addr := ln.Addr().(*net.TCPAddr)
	srv.host, srv.port = addr.IP.String(), addr.Port
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

func (srv *jumpSSH) serve(conn net.Conn, cfg *ssh.ServerConfig) {
	sconn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		conn.Close()
		return
	}
	srv.open.Add(1)
	defer srv.open.Add(-1)
	defer sconn.Close()
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		var to struct {
			Host       string
			Port       uint32
			OriginHost string
			OriginPort uint32
		}
		if nc.ChannelType() != "direct-tcpip" || ssh.Unmarshal(nc.ExtraData(), &to) != nil {
			nc.Reject(ssh.Prohibited, "no")
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
		go func() {
			var once sync.Once
			done := func() { once.Do(func() { ch.Close(); out.Close() }) }
			go func() { io.Copy(ch, out); done() }()
			io.Copy(out, ch)
			done()
		}()
	}
}

func jumpTo(id string) *config.ProxyConfig {
	return &config.ProxyConfig{Kind: config.ProxyJump, JumpHost: id}
}

// answerHostKeys trusts every host key s is asked about, keeping the
// hosts, until s is connected or failed.
func answerHostKeys(t *testing.T, p *fakePlatform, s *Session) []string {
	t.Helper()
	var asked []string
	p.until(t, "the connection", func() bool {
		if q := s.hostKeyQ; q != nil {
			asked = append(asked, q.Host)
			q.Answer <- true
			s.hostKeyQ = nil
		}
		return s.status == statusConnected || s.status == statusFailed
	})
	return asked
}

// TestConnectThroughJumpHost connects to a host through a jump host,
// asking for both passwords first, one after the other, and trusting
// both host keys; closing the session closes the jump host's connection.
func TestConnectThroughJumpHost(t *testing.T) {
	target := startFakeSSH(t)
	jump := startJumpSSH(t)
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	mainWin := ui.NewTester(a.View, 1280, 820)
	p.tt = mainWin
	j := &config.Host{ID: "j", Name: "jump-1", Addr: jump.host, Port: jump.port, User: "hop", Auth: config.AuthPassword}
	h := &config.Host{ID: "t", Name: "app-1", Addr: target.host, Port: target.port, User: "u", Auth: config.AuthPassword,
		NoMonitor: true, Proxy: jumpTo("j")}
	a.hosts.Add(j)
	a.hosts.Add(h)

	a.openSession(h)
	// Once the credential store was read, which has neither.
	p.until(t, "the prompt", func() bool { return a.pwOpen })
	mainWin.Frame()
	if !a.pwOpen || a.pwNeed.key != "t" || !mainWin.HasText(locales.EnterPassword()) || !mainWin.HasText(h.Target()) {
		t.Fatalf("first prompt %+v; texts %q", a.pwNeed, mainWin.Texts())
	}
	mainWin.Type("secret")
	mainWin.Key(0, ui.KeyEnter)
	mainWin.Frame()
	// Then the jump host's, saying what for.
	jumpWho := "jump-1 (" + j.Target() + ")"
	if !a.pwOpen || a.pwNeed == nil || a.pwNeed.key != "j" || !mainWin.HasText(locales.JumpHostPasswordTitle()) ||
		!mainWin.HasText(jumpWho) || !mainWin.HasText(locales.PasswordOnTheWay("app-1")) || len(a.sessions) != 0 {
		t.Fatalf("jump prompt %+v; texts %q", a.pwNeed, mainWin.Texts())
	}
	saveShot(t, mainWin, "password-jump-host-en.png")
	p.SetLocale("zh-Hans")
	mainWin.Frame()
	if !mainWin.HasText("输入跳板机密码") || !mainWin.HasText("连接 app-1 时需要") {
		t.Errorf("not in Chinese; texts %q", mainWin.Texts())
	}
	saveShot(t, mainWin, "password-jump-host-zh-Hans.png")
	p.SetLocale("")
	mainWin.Type("secret")
	mainWin.Key(0, ui.KeyEnter)
	if a.pwOpen || a.passwords["t"] != "secret" || a.passwords["j"] != "secret" {
		t.Fatalf("prompt open %v, passwords %v", a.pwOpen, a.passwords)
	}

	s := a.current()
	asked := answerHostKeys(t, p, s)
	if s.status != statusConnected {
		t.Fatal(describeError(s.err))
	}
	// The jump host's key first, then the host's.
	want := []string{net.JoinHostPort(jump.host, strconv.Itoa(jump.port)), net.JoinHostPort(target.host, strconv.Itoa(target.port))}
	if !slices.Equal(asked, want) || s.ip != target.host || jump.open.Load() != 1 {
		t.Errorf("asked %v, ip %q, jump connections %d", asked, s.ip, jump.open.Load())
	}

	// Reconnecting asks nothing more.
	a.reconnect(s)
	if a.pwOpen {
		t.Fatal("asked again")
	}
	if asked := answerHostKeys(t, p, s); s.status != statusConnected || len(asked) != 0 {
		t.Fatalf("reconnect: %v, asked %v", describeError(s.err), asked)
	}
	p.until(t, "the old jump connection closed", func() bool { return jump.open.Load() == 1 })

	a.closeSession(s)
	p.until(t, "the jump host's connection closed", func() bool { return jump.open.Load() == 0 })
}

// Errors on the way name the hop that failed, in the language shown.
func TestJumpHostErrors(t *testing.T) {
	target := startFakeSSH(t)
	jump := startJumpSSH(t)
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	j := &config.Host{ID: "j", Name: "jump-1", Addr: jump.host, Port: jump.port, User: "hop", Auth: config.AuthPassword}
	h := &config.Host{ID: "t", Name: "app-1", Addr: target.host, Port: target.port, User: "u", Auth: config.AuthPassword,
		NoMonitor: true, Proxy: jumpTo("j")}
	a.hosts.Add(j)
	a.hosts.Add(h)
	a.passwords["t"], a.passwords["j"] = "secret", "wrong"

	a.openSession(h)
	s := a.current()
	answerHostKeys(t, p, s)
	if s.status != statusFailed {
		t.Fatal("connected with a wrong jump host password")
	}
	if got, want := describeError(s.err), locales.ErrViaJump("jump-1", locales.ErrAuthFailed()); got != want {
		t.Errorf("wrong password: %q, want %q", got, want)
	}
	// The prompt asks for the jump host's password again.
	if !a.pwOpen || a.pwNeed.key != "j" || a.pwErr != s.err {
		t.Errorf("prompt %v %+v, err %v", a.pwOpen, a.pwNeed, a.pwErr)
	}
	a.pwOpen = false
	p.SetLocale("zh-Hans")
	if got := describeError(s.err); got != "跳板机 jump-1：认证失败：用户名、密码或私钥不正确" {
		t.Errorf("in Chinese: %q", got)
	}
	p.SetLocale("")

	// The jump host cannot reach the host.
	a.passwords["j"] = "secret"
	h.Port = closedPort(t)
	a.reconnect(s)
	answerHostKeys(t, p, s)
	addr := net.JoinHostPort(target.host, strconv.Itoa(h.Port))
	if got, want := describeError(s.err), locales.ErrViaJump("jump-1", locales.ErrCannotReach(addr, "Connection refused")); got != want {
		t.Errorf("forwarding: %q, want %q", got, want)
	}

	// A loop fails at once, asking nothing.
	j.Proxy = jumpTo("t")
	a.reconnect(s)
	if s.status != statusFailed || a.pwOpen {
		t.Fatalf("loop: status %d, prompt %v", s.status, a.pwOpen)
	}
	if got, want := describeError(s.err), locales.ErrJumpLoop("app-1 → jump-1 → app-1"); got != want {
		t.Errorf("loop: %q, want %q", got, want)
	}
	// So does a jump host that was deleted.
	j.Proxy = nil
	a.hosts.SoftDelete("j", time.Now())
	a.reconnect(s)
	if got, want := describeError(s.err), locales.ErrJumpHostMissing("app-1"); got != want {
		t.Errorf("deleted: %q, want %q", got, want)
	}
}

// The passwords asked for before connecting, in the order they are on
// the way: the host's, its proxy's, then each jump host's and its
// proxy's.
func TestMissingSecrets(t *testing.T) {
	a, p := testApp(t)
	socks := &config.ProxyConfig{Kind: config.ProxySOCKS5, Addr: "198.51.100.7", Port: 1080, User: "alice"}
	outer := &config.Host{ID: "o", Name: "outer", Addr: "203.0.113.1", User: "o", Auth: config.AuthPassword, Proxy: socks}
	mid := &config.Host{ID: "m", Addr: "10.0.0.2", User: "m", Auth: config.AuthKey, KeyPath: "k", Proxy: jumpTo("o")}
	h := &config.Host{ID: "t", Name: "app-1", Addr: "10.0.0.3", User: "u", Auth: config.AuthPassword,
		Proxy: &config.ProxyConfig{Kind: config.ProxyHTTP, Addr: "proxy", User: "bob"}}
	a.hosts.Add(outer)
	a.hosts.Add(mid)
	a.hosts.Add(h)

	type need struct{ key, title, who, via string }
	var got []need
	for n := a.missingSecret(h); n != nil && len(got) < 10; n = a.missingSecret(h) {
		got = append(got, need{n.key, n.title(), n.who, n.via})
		a.keepSecret(n, "pw-"+n.key)
	}
	want := []need{
		{"t", locales.EnterPassword(), "u@10.0.0.3", ""},
		{"proxy/t", locales.ProxyPasswordTitle(), "bob@proxy:8080", "app-1"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("direct proxy: %+v, want %+v", got, want)
	}

	// Through the jump hosts: mid logs in with a key, which asks nothing.
	h.Proxy = jumpTo("m")
	got = nil
	for n := a.missingSecret(h); n != nil && len(got) < 10; n = a.missingSecret(h) {
		got = append(got, need{n.key, n.title(), n.who, n.via})
		a.keepSecret(n, "pw-"+n.key)
	}
	want = []need{
		{"o", locales.JumpHostPasswordTitle(), "outer (o@203.0.113.1)", "app-1"},
		{"proxy/o", locales.ProxyPasswordTitle(), "alice@198.51.100.7:1080", "app-1"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("jump hosts: %+v, want %+v", got, want)
	}

	// What dialing takes: each hop with its own password, and the
	// proxies' passwords.
	opts, err := a.dialOptions(h, "pw-t", nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Password != "pw-t" || opts.ProxyPassword != "" || opts.Jump == nil || opts.Jump.Host.ID != "m" {
		t.Fatalf("options %+v", opts)
	}
	m := opts.Jump
	if m.Options.Passphrase != "" || m.Options.Jump == nil || m.Host == mid {
		t.Errorf("mid %+v (a copy: %v)", m.Options, m.Host != mid)
	}
	o := m.Options.Jump
	if o.Host.ID != "o" || o.Options.Password != "pw-o" || o.Options.ProxyPassword != "pw-proxy/o" || o.Options.Jump != nil {
		t.Errorf("outer %+v", o.Options)
	}

	// A host edited to go through a jump host whose password is not
	// known asks for it as it reconnects.
	a.passwords = map[string]string{"t": "pw"}
	h.Proxy = nil
	s := newSession(h)
	s.status = statusClosed
	a.sessions = append(a.sessions, s)
	h.Proxy = jumpTo("o")
	a.reconnect(s)
	p.until(t, "the prompt", func() bool { return a.pwOpen })
	if !a.pwOpen || a.pwNeed.key != "o" || s.status != statusClosed {
		t.Errorf("reconnect: prompt %v %+v, status %d", a.pwOpen, a.pwNeed, s.status)
	}
}

// TestSSHJumpHost goes through the server FINALSHELLY_TEST_SSH names to
// itself, as the session's jump host, and runs a command in the shell.
func TestSSHJumpHost(t *testing.T) {
	spec := os.Getenv("FINALSHELLY_TEST_SSH")
	if spec == "" {
		t.Skip("FINALSHELLY_TEST_SSH not set")
	}
	cred, addr, _ := strings.Cut(spec, "@")
	user, pass, _ := strings.Cut(cred, ":")
	hostname, portStr, _ := strings.Cut(addr, ":")
	port, _ := strconv.Atoi(portStr)

	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	j := &config.Host{ID: "j", Name: "live-jump", Addr: hostname, Port: port, User: user, Auth: config.AuthPassword, NoMonitor: true}
	h := &config.Host{ID: "t", Name: "live", Addr: hostname, Port: port, User: user, Auth: config.AuthPassword, Proxy: jumpTo("j")}
	a.hosts.Add(j)
	a.hosts.Add(h)
	a.passwords["j"], a.passwords["t"] = pass, pass
	a.openSession(h)
	s := a.current()
	// The same server twice: its key is asked about once.
	if asked := answerHostKeys(t, p, s); s.status != statusConnected || len(asked) != 1 {
		t.Fatalf("status %d (%s), asked %v", s.status, describeError(s.err), asked)
	}
	s.term.Send([]byte("echo jumped-$((40+2))\r"))
	p.until(t, "the shell's output", func() bool { return strings.Contains(s.term.Text(), "jumped-42") })
	if s.ip != hostname {
		t.Errorf("ip %q", s.ip)
	}
	p.until(t, "the server's stats", func() bool { return s.stats.OK })
	p.until(t, "the home directory", func() bool { return s.files.cwd != "" && !s.files.loading })

	// A wrong jump host password names the jump host.
	a.passwords["j"] = "wrong-" + pass
	a.reconnect(s)
	answerHostKeys(t, p, s)
	if got := describeError(s.err); got != locales.ErrViaJump("live-jump", locales.ErrAuthFailed()) {
		t.Errorf("wrong jump password: %q", got)
	}
}

func TestDescribeRouteErrors(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}
	for _, c := range []struct {
		err    error
		en, zh string
	}{
		{&sshx.HopError{Proxy: "1.2.3.4:1080", Err: refused},
			"Proxy 1.2.3.4:1080: Connection refused: no proxy on that port", "代理 1.2.3.4:1080：连接被拒绝：端口没有开放代理服务"},
		{&sshx.HopError{Proxy: "1.2.3.4:1080", Err: sshx.ErrProxyAuth},
			"Proxy 1.2.3.4:1080: Authentication failed: wrong proxy user or password", "代理 1.2.3.4:1080：认证失败：代理用户名或密码不正确"},
		{&sshx.HopError{Proxy: "proxy:8080", Err: &sshx.ProxyTargetError{Addr: "10.0.0.1:22", Reason: "502 Bad Gateway"}},
			"Proxy proxy:8080: Cannot connect to 10.0.0.1:22: 502 Bad Gateway", "代理 proxy:8080：无法连接到 10.0.0.1:22：502 Bad Gateway"},
		{&sshx.HopError{Proxy: "1.2.3.4:1080", Err: os.ErrDeadlineExceeded},
			"Proxy 1.2.3.4:1080: " + locales.ErrTimeout(), ""},
		{&sshx.HopError{JumpHost: "web-1", Err: &sshx.ForwardError{Addr: "10.0.0.1:22", Err: &ssh.OpenChannelError{Reason: ssh.Prohibited, Message: "administratively prohibited"}}},
			"Jump host web-1: Cannot connect to 10.0.0.1:22: the jump host does not allow TCP forwarding (AllowTcpForwarding)",
			"跳板机 web-1：无法连接到 10.0.0.1:22：跳板机不允许 TCP 转发（AllowTcpForwarding）"},
		{&sshx.HopError{JumpHost: "web-1", Err: sshx.ErrHostKeyRejected},
			"Jump host web-1: The host key was rejected", ""},
	} {
		i18nLocale(t, "en")
		if got := describeError(c.err); got != c.en {
			t.Errorf("%v: %q, want %q", c.err, got, c.en)
		}
		if c.zh == "" {
			continue
		}
		i18nLocale(t, "zh-Hans")
		if got := describeError(c.err); got != c.zh {
			t.Errorf("%v in Chinese: %q, want %q", c.err, got, c.zh)
		}
	}
}

// i18nLocale shows tag for the rest of the test.
func i18nLocale(t *testing.T, tag string) {
	p := newFakePlatform(t)
	p.SetLocale(tag)
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
