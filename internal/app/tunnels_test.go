package app

import (
	"encoding/binary"
	"io"
	"net"
	"runtime"
	"strconv"
	"testing"
	"time"

	"finalshelly/internal/config"
	"finalshelly/internal/sshtest"
	"finalshelly/internal/sshx"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
)

// echoServer listens on the loopback and sends back what it reads.
func echoServer(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				io.Copy(c, c)
				c.Close()
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// freeLocalPort is a port nothing listens on, on the loopback.
func freeLocalPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// echoes checks that msg sent to port, through SOCKS5 to the target
// port when socks is set, comes back.
func echoes(t *testing.T, port int, socks int, msg string) {
	t.Helper()
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	if socks != 0 {
		c.Write([]byte{5, 1, 0})
		reply := make([]byte, 2)
		io.ReadFull(c, reply)
		c.Write(binary.BigEndian.AppendUint16([]byte{5, 1, 0, 3, 9, 'l', 'o', 'c', 'a', 'l', 'h', 'o', 's', 't'}, uint16(socks)))
		reply = make([]byte, 10)
		if _, err := io.ReadFull(c, reply); err != nil || reply[1] != 0 {
			t.Fatalf("SOCKS reply %v %v", reply, err)
		}
	}
	c.Write([]byte(msg))
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(c, got); err != nil || string(got) != msg {
		t.Fatalf("port %d echoed %q, %v", port, got, err)
	}
}

// portFree tells whether nothing listens on port of the loopback.
func portFree(port int) bool {
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// tunnelHost is a host of srv with a tunnel of each kind to the echo
// server, one on a port taken, and one disabled.
func tunnelHost(srv *sshtest.Server, echo, taken int, ports [4]int) *config.Host {
	return &config.Host{ID: "tun", Name: "tunnels", Addr: srv.Host, Port: srv.Port, User: "tester", Auth: config.AuthPassword, NoMonitor: true,
		Tunnels: []config.Tunnel{
			{Kind: config.TunnelLocal, ListenPort: ports[0], TargetHost: "127.0.0.1", TargetPort: echo},
			{Kind: config.TunnelRemote, ListenPort: ports[1], TargetHost: "127.0.0.1", TargetPort: echo},
			{Kind: config.TunnelDynamic, ListenPort: ports[2]},
			{Kind: config.TunnelLocal, ListenPort: taken, TargetHost: "127.0.0.1", TargetPort: echo},
			{Kind: config.TunnelLocal, ListenPort: ports[3], TargetHost: "127.0.0.1", TargetPort: echo, Disabled: true},
		}}
}

// tunnelsSettled tells whether s's tunnels started, or failed to.
func tunnelsSettled(s *Session, n int) bool {
	runs := s.tunnels.runs
	if len(runs) != n {
		return false
	}
	for _, r := range runs {
		if r.starting {
			return false
		}
	}
	return true
}

func TestTunnelsLifecycle(t *testing.T) {
	srv := sshtest.Start(t)
	echo := echoServer(t)
	takenLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer takenLn.Close()
	taken := takenLn.Addr().(*net.TCPAddr).Port
	ports := [4]int{freeLocalPort(t), freeLocalPort(t), freeLocalPort(t), freeLocalPort(t)}

	a, p := testApp(t)
	h := tunnelHost(srv, echo, taken, ports)
	a.hosts.Add(h)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	a.bottomTab = 2
	tt.Frame() // the drawing starts goroutines of its own, for good
	base := runtime.NumGoroutine()
	s := connectFake(t, a, p, h)
	p.until(t, "the tunnels", func() bool { return tunnelsSettled(s, 5) })
	runs := s.tunnels.runs
	for i, listens := range []bool{true, true, true, false, false} {
		if (runs[i].fwd != nil) != listens {
			t.Errorf("tunnel %d: listening %v, err %v", i, runs[i].fwd != nil, runs[i].err)
		}
	}
	// The port taken fails its tunnel alone.
	if s.status != statusConnected || runs[3].err == nil || runs[4].err != nil {
		t.Fatalf("status %d; errors %v, %v", s.status, runs[3].err, runs[4].err)
	}
	inUse := locales.ErrTunnelInUse("127.0.0.1:" + strconv.Itoa(taken))
	if got := describeTunnelError(runs[3].err); got != inUse {
		t.Errorf("error %q", got)
	}

	// Data goes both ways through each.
	echoes(t, ports[0], 0, "through -L")
	echoes(t, ports[1], 0, "through -R")
	echoes(t, ports[2], echo, "through -D")
	p.until(t, "the stats shown", func() bool {
		return tt.HasText(locales.TunnelConnectionsCount(0, 1)) && tt.HasText(locales.TunnelTraffic(formatSize(10), formatSize(10)))
	})
	for _, want := range []string{locales.TabTunnelsActive(3), locales.TunnelListening(), inUse, locales.TunnelStopped(),
		locales.TunnelSocks(), "127.0.0.1:" + strconv.Itoa(ports[1])} {
		if !tt.HasText(want) {
			t.Errorf("missing %q; texts %q", want, tt.Texts())
		}
	}

	for _, lang := range []string{"en", "zh-Hans"} {
		p.SetLocale(lang)
		tt.Frame()
		saveShot(t, tt, "tunnels-status-"+lang+".png")
	}
	p.SetLocale("")
	tt.Frame()

	// Started and stopped by hand: the one that failed fails again, the
	// disabled one listens, and stops.
	if err := tt.Click(locales.TunnelStart()); err != nil { // the first: the port taken
		t.Fatal(err)
	}
	p.until(t, "the retry", func() bool { return !runs[3].starting })
	if runs[3].err == nil || runs[3].fwd != nil {
		t.Errorf("retry: %v", runs[3].err)
	}
	if err := tt.Click(locales.TunnelStart()); err != nil {
		t.Fatal(err)
	}
	if !runs[3].starting {
		t.Fatal("start did not start")
	}
	runs[3].stop() // stopped while it starts: the late start is undone
	p.until(t, "the late start", func() bool { return len(p.posts) == 0 })
	if runs[3].fwd != nil || runs[3].err != nil {
		t.Errorf("stopped while starting: %v %v", runs[3].fwd, runs[3].err)
	}
	a.startTunnel(s, runs[4])
	p.until(t, "the disabled tunnel", func() bool { return runs[4].fwd != nil })
	echoes(t, ports[3], 0, "by hand")
	runs[4].stop()
	if !portFree(ports[3]) {
		t.Error("stopping kept the port")
	}

	// Disconnecting stops them all and frees the ports, here and on the
	// server.
	var fwds []*sshx.Tunnel
	for _, r := range runs {
		if r.fwd != nil {
			fwds = append(fwds, r.fwd)
		}
	}
	s.disconnect()
	s.status = statusClosed
	for i, r := range runs {
		if r.fwd != nil || r.starting {
			t.Errorf("tunnel %d still runs", i)
		}
	}
	waitTunnels(t, fwds)
	if !portFree(ports[0]) || !portFree(ports[2]) {
		t.Error("the ports were kept")
	}
	p.until(t, "the server's ports", func() bool { return srv.Forwards() == 0 })
	tt.Frame()
	if !tt.HasText(locales.TunnelsOffline()) || !tt.HasText(locales.TabTunnels()) || tt.HasText(locales.TunnelListening()) {
		t.Errorf("after disconnecting; texts %q", tt.Texts())
	}
	saveShot(t, tt, "tunnels-offline-en.png")

	// Reconnecting starts them again, on the same ports.
	a.reconnect(s)
	p.until(t, "the reconnection", func() bool { return s.status == statusConnected || s.status == statusFailed })
	p.until(t, "the tunnels again", func() bool { return tunnelsSettled(s, 5) && s.tunnels.runs[0].fwd != nil })
	if s.tunnels.runs[0] == runs[0] {
		t.Error("the tunnels were not started afresh")
	}
	runs = s.tunnels.runs
	echoes(t, ports[0], 0, "again")
	echoes(t, ports[1], 0, "again")

	// Losing the connection stops them too.
	fwds = fwds[:0]
	for _, r := range runs {
		if r.fwd != nil {
			fwds = append(fwds, r.fwd)
		}
	}
	srv.DropConnections()
	p.until(t, "the connection lost", func() bool { return s.status == statusClosed })
	for i, r := range runs {
		if r.fwd != nil {
			t.Errorf("tunnel %d still runs", i)
		}
	}
	waitTunnels(t, fwds)
	if !portFree(ports[0]) || !portFree(ports[2]) {
		t.Error("the ports were kept")
	}

	// And nothing is left running.
	a.CloseAll()
	srv.Close()
	deadline := time.Now().Add(10 * time.Second)
	for runtime.NumGoroutine() > base {
		p.drain()
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			t.Fatalf("%d goroutines, %d before:\n%s", runtime.NumGoroutine(), base, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitTunnels waits until the tunnels' goroutines are gone.
func waitTunnels(t *testing.T, fwds []*sshx.Tunnel) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		for _, f := range fwds {
			f.Wait()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the tunnels did not stop")
	}
}

// The tab says when the session has no tunnels, and its button opens the
// editor on them.
func TestTunnelPanelEmpty(t *testing.T) {
	a, p := testApp(t)
	s := demoSession(a, "web-1", "203.0.113.10")
	a.active = 0
	a.bottomTab = 2
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	if !tt.HasText(locales.TunnelsOffline()) {
		t.Errorf("texts %q", tt.Texts())
	}
	s.client = nil
	if err := tt.Click(locales.TunnelsConfigure()); err != nil {
		t.Fatal(err)
	}
	if !a.windowOpen(WindowEditor) || a.editor.id != s.Host.ID || a.editor.nav.Page != editorTunnels {
		t.Fatalf("editor %v on %q", a.windowOpen(WindowEditor), a.editor.nav.Page)
	}
}

// TestSSHTunnels forwards through the real server FINALSHELLY_TEST_SSH
// names, which has to be this machine, with TCP forwarding allowed:
//
//	FINALSHELLY_TEST_SSH=tester:secret@127.0.0.1:2222 go test ./internal/app -run SSHTunnels
func TestSSHTunnels(t *testing.T) {
	a, p, s := connectTestServer(t)
	t.Cleanup(a.CloseAll)
	echo := echoServer(t)
	takenLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer takenLn.Close()
	taken := takenLn.Addr().(*net.TCPAddr).Port
	ports := [3]int{freeLocalPort(t), freeLocalPort(t), freeLocalPort(t)}
	s.Host.Tunnels = []config.Tunnel{
		{Kind: config.TunnelLocal, ListenPort: ports[0], TargetHost: "127.0.0.1", TargetPort: echo},
		{Kind: config.TunnelRemote, ListenPort: ports[1], TargetHost: "127.0.0.1", TargetPort: echo},
		{Kind: config.TunnelDynamic, ListenPort: ports[2]},
		{Kind: config.TunnelLocal, ListenPort: taken, TargetHost: "127.0.0.1", TargetPort: echo},
		{Kind: config.TunnelRemote, ListenPort: taken, TargetHost: "127.0.0.1", TargetPort: echo},
	}
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	a.bottomTab = 2
	for round := range 2 { // as it connects, and again after disconnecting
		a.reconnect(s)
		p.until(t, "the tunnels", func() bool { return s.status == statusConnected && tunnelsSettled(s, 5) })
		runs := s.tunnels.runs
		for i, listens := range []bool{true, true, true, false, false} {
			if (runs[i].fwd != nil) != listens {
				t.Fatalf("round %d, tunnel %d: listening %v, err %v", round, i, runs[i].fwd != nil, runs[i].err)
			}
		}
		addr := "127.0.0.1:" + strconv.Itoa(taken)
		if describeTunnelError(runs[3].err) != locales.ErrTunnelInUse(addr) || describeTunnelError(runs[4].err) != locales.ErrTunnelRemoteDenied(addr) {
			t.Errorf("errors %q, %q", describeTunnelError(runs[3].err), describeTunnelError(runs[4].err))
		}
		echoes(t, ports[0], 0, "live -L")
		echoes(t, ports[1], 0, "live -R")
		echoes(t, ports[2], echo, "live -D")
		if round == 0 {
			p.until(t, "the stats", func() bool { return tt.HasText(locales.TunnelConnectionsCount(0, 1)) })
			for _, lang := range []string{"en", "zh-Hans"} {
				p.SetLocale(lang)
				tt.Frame()
				saveShot(t, tt, "tunnels-status-live-"+lang+".png")
			}
			p.SetLocale("")
		}
		var fwds []*sshx.Tunnel
		for _, r := range runs {
			if r.fwd != nil {
				fwds = append(fwds, r.fwd)
			}
		}
		s.disconnect()
		waitTunnels(t, fwds)
		if !portFree(ports[0]) || !portFree(ports[2]) {
			t.Error("the local ports were kept")
		}
		p.until(t, "the server to free its port", func() bool { return portFree(ports[1]) })
	}

	// Through the same server as its own jump host: the tunnels use the
	// last connection, to the host.
	j := &config.Host{ID: "live-jump", Name: "live-jump", Addr: s.Host.Addr, Port: s.Host.Port, User: s.Host.User, Auth: config.AuthPassword, NoMonitor: true}
	a.hosts.Add(j)
	a.passwords[j.ID] = a.passwords[s.Host.ID]
	s.Host.Proxy = jumpTo(j.ID)
	a.reconnect(s)
	answerHostKeys(t, p, s)
	p.until(t, "the tunnels through the jump host", func() bool { return s.status == statusConnected && tunnelsSettled(s, 5) })
	if s.tunnels.runs[0].fwd == nil || s.tunnels.runs[1].fwd == nil || s.tunnels.runs[2].fwd == nil {
		t.Fatalf("not listening: %v %v %v", s.tunnels.runs[0].err, s.tunnels.runs[1].err, s.tunnels.runs[2].err)
	}
	echoes(t, ports[0], 0, "jumped -L")
	echoes(t, ports[1], 0, "jumped -R")
	echoes(t, ports[2], echo, "jumped -D")
}

// TestTunnelsThroughJumpHost forwards through a host reached by way of
// a jump host; closing the session frees the ports.
func TestTunnelsThroughJumpHost(t *testing.T) {
	jump, target := sshtest.Start(t), sshtest.Start(t)
	echo := echoServer(t)
	ports := [4]int{freeLocalPort(t), freeLocalPort(t), freeLocalPort(t), freeLocalPort(t)}
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	j := &config.Host{ID: "j", Name: "jump-1", Addr: jump.Host, Port: jump.Port, User: "hop", Auth: config.AuthPassword, NoMonitor: true}
	h := tunnelHost(target, echo, freeLocalPort(t), ports)
	h.Proxy = jumpTo("j")
	a.hosts.Add(j)
	a.hosts.Add(h)
	a.passwords["j"], a.passwords[h.ID] = sshtest.Password, sshtest.Password
	a.openSession(h)
	s := a.current()
	if answerHostKeys(t, p, s); s.status != statusConnected {
		t.Fatal(describeError(s.err))
	}
	p.until(t, "the tunnels", func() bool { return tunnelsSettled(s, 5) && s.tunnels.runs[0].fwd != nil })
	echoes(t, ports[0], 0, "jumped -L")
	echoes(t, ports[1], 0, "jumped -R")
	echoes(t, ports[2], echo, "jumped -D")
	if target.Forwards() != 1 || jump.Forwards() != 0 {
		t.Errorf("forwards: target %d, jump host %d", target.Forwards(), jump.Forwards())
	}
	a.closeSession(s)
	p.until(t, "the ports freed", func() bool { return portFree(ports[0]) && portFree(ports[2]) && target.Forwards() == 0 })
}
